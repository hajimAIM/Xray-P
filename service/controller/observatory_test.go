package controller_test

import (
	"encoding/json"
	"fmt"
	stdnet "net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/xtls/xray-core/app/dispatcher"
	"github.com/xtls/xray-core/app/log"
	"github.com/xtls/xray-core/app/proxyman"
	"github.com/xtls/xray-core/app/stats"
	comlog "github.com/xtls/xray-core/common/log"
	xraynet "github.com/xtls/xray-core/common/net"
	"github.com/xtls/xray-core/common/serial"
	"github.com/xtls/xray-core/core"
	"github.com/xtls/xray-core/infra/conf"
	"github.com/xtls/xray-core/infra/conf/cfgcommon/duration"

	"Xray-P/api"
	"Xray-P/api/sspanel"
	_ "Xray-P/cmd/distro/all"
	. "Xray-P/service/controller"
)

func getFreePortVal() int {
	addr, _ := stdnet.ResolveTCPAddr("tcp", "localhost:0")
	l, _ := stdnet.ListenTCP("tcp", addr)
	defer l.Close()
	return l.Addr().(*stdnet.TCPAddr).Port
}

func TestObservatoryVless(t *testing.T) {
	// 1. Setup Mock Backends
	// Fast Backend: Returns 200 OK "FAST" immediately
	backendFast := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("FAST"))
	}))
	defer backendFast.Close()

	_, portFastStr, _ := stdnet.SplitHostPort(backendFast.Listener.Addr().String())
	portFastInt := 0
	fmt.Sscanf(portFastStr, "%d", &portFastInt)
	t.Logf("Backend Fast Port: %d", portFastInt)

	// Verify Direct Connectivity
	resp, err := http.Get(backendFast.URL)
	if err != nil {
		t.Fatalf("Direct connection to backend failed: %v", err)
	}
	resp.Body.Close()
	t.Log("Direct connection to backend successful.")

	// Slow/Fail Backend: A closed port to simulate failure for "leastPing"
	portSlowInt := getFreePortVal()
	// We don't listen on it, so connection will be refused.

	// 2. Setup Mock SSPanel Server
	serverPort := getFreePortVal()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Logf("Mock Server Request: %s %s", r.Method, r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/mod_mu/nodes/42/info" {
			nodeInfo := sspanel.NodeInfoResponse{
				RawServerString: fmt.Sprintf("127.0.0.1;%d;0;none;tcp;server=127.0.0.1|host=vless.test.com", serverPort),
				CustomConfig:    json.RawMessage(fmt.Sprintf(`{"offset_port_node": "%d", "enable_vless": "1", "vless_flow": "", "network": "tcp"}`, serverPort)),
				Type:            "1",
				Version:         "2021.11",
			}
			nodeInfoBytes, _ := json.Marshal(nodeInfo)
			json.NewEncoder(w).Encode(sspanel.Response{Ret: 1, Data: json.RawMessage(nodeInfoBytes)})
			return
		}
		if r.URL.Path == "/mod_mu/users" {
			users := []sspanel.UserResponse{{
				ID: 1001, UUID: "b831381d-6324-4d53-ad4f-8cda48b30811", Port: uint32(serverPort), Method: "aes-128-gcm",
			}}
			usersBytes, _ := json.Marshal(users)
			json.NewEncoder(w).Encode(sspanel.Response{Ret: 1, Data: json.RawMessage(usersBytes)})
			return
		}
		// AliveIP reporting
		if r.URL.Path == "/mod_mu/users/aliveip" {
			json.NewEncoder(w).Encode(struct {
				Ret int `json:"ret"`
			}{Ret: 1})
			return
		}
		// Rule detection
		if r.URL.Path == "/mod_mu/func/detect_rules" {
			json.NewEncoder(w).Encode(struct {
				Ret  int           `json:"ret"`
				Data []interface{} `json:"data"`
			}{Ret: 1, Data: []interface{}{}})
			return
		}
		json.NewEncoder(w).Encode(struct {
			Ret int `json:"ret"`
		}{Ret: 1})
	}))
	defer ts.Close()

	// 3. Create Config Files for Xray-P Controller
	tempDir, err := os.MkdirTemp("", "test_obs")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		os.RemoveAll(tempDir)
	}()
	accessLogPath := filepath.Join(tempDir, "access.log")
	errorLogPath := filepath.Join(tempDir, "error.log")
	observatoryJSON := filepath.Join(tempDir, "observatory.json")
	routeJSON := filepath.Join(tempDir, "route.json")
	outboundJSON := filepath.Join(tempDir, "outbound.json")

	// Observatory Config
	obsConfig := conf.ObservatoryConfig{
		SubjectSelector:   []string{"out_fast", "out_slow"},
		ProbeURL:          backendFast.URL,
		ProbeInterval:     duration.Duration(2 * time.Second),
		EnableConcurrency: true,
	}
	obsBytes, _ := json.Marshal(obsConfig)
	os.WriteFile(observatoryJSON, obsBytes, 0644)

	// manually build obsevatory config message for core injection
	coreObservatoryConfigMsg, _ := obsConfig.Build()

	// Outbound Config
	// out_fast: Freedom (works)
	// out_slow: Socks to closed port (fails)
	outFast := conf.OutboundDetourConfig{Tag: "out_fast", Protocol: "freedom"}
	outFallback := conf.OutboundDetourConfig{Tag: "out_fallback", Protocol: "freedom"}

	outSlow := conf.OutboundDetourConfig{Tag: "out_slow", Protocol: "socks"}
	outSlowSettings := fmt.Sprintf(`{"servers": [{"address": "127.0.0.1", "port": %d}]}`, portSlowInt)
	rawSetting := json.RawMessage(outSlowSettings)
	outSlow.Settings = &rawSetting

	outObjs := []conf.OutboundDetourConfig{outFast, outSlow, outFallback}
	outBytes, _ := json.Marshal(outObjs)
	os.WriteFile(outboundJSON, outBytes, 0644)

	// Router Config
	balancer := conf.BalancingRule{
		Tag:         "balancer_test",
		Selectors:   []string{"out_fast", "out_slow"},
		Strategy:    conf.StrategyConfig{Type: "leastping"},
		FallbackTag: "out_fallback",
	}

	// Rule: Match traffic to backend port and route to balancer
	// We want to force traffic through balancer.
	ruleMap := map[string]interface{}{
		"balancerTag": "balancer_test",
		"port":        fmt.Sprintf("%d", portFastInt),
	}
	ruleBytes, _ := json.Marshal(ruleMap)

	routerConfig := conf.RouterConfig{
		Balancers: []*conf.BalancingRule{&balancer},
		RuleList:  []json.RawMessage{json.RawMessage(ruleBytes)},
	}
	routerBytes, _ := json.Marshal(routerConfig)
	os.WriteFile(routeJSON, routerBytes, 0644)

	// Manually build messages for core injection
	// For routing, controller loads it from file defined in mainParams if we were mocking panel.
	// But since we are creating core manually:
	coreRouterConfigMsg, _ := routerConfig.Build()

	// For outbound, created above.
	var coreOutbounds []*core.OutboundHandlerConfig
	for _, o := range outObjs {
		ob, _ := o.Build()
		coreOutbounds = append(coreOutbounds, ob)
	}

	// 4. Start Xray-P Controller
	logConfig := &log.Config{
		ErrorLogType:  log.LogType_File,
		ErrorLogPath:  errorLogPath,
		ErrorLogLevel: comlog.Severity_Debug,
		AccessLogType: log.LogType_File,
		AccessLogPath: accessLogPath,
	}
	serverConfig := &core.Config{
		App: []*serial.TypedMessage{
			serial.ToTypedMessage(logConfig),
			serial.ToTypedMessage(&dispatcher.Config{}),
			serial.ToTypedMessage(&proxyman.InboundConfig{}),
			serial.ToTypedMessage(&proxyman.OutboundConfig{}),
			serial.ToTypedMessage(&stats.Config{}),
			serial.ToTypedMessage(coreObservatoryConfigMsg), // Inject Observatory
			serial.ToTypedMessage(coreRouterConfigMsg),      // Inject Router
		},
		Outbound: coreOutbounds, // Inject custom outbounds
	}

	serverInstance, err := core.New(serverConfig)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}

	apiClient := sspanel.New(&api.Config{
		APIHost:  ts.URL,
		NodeID:   42,
		NodeType: "V2ray",
		Key:      "val",
	})

	// Controller Config - Only valid fields
	mainParams := &Config{
		ListenIP:        "127.0.0.1",
		UpdatePeriodic:  60,
		EnableDNS:       false,
		DisableSniffing: true,
	}

	c := New(serverInstance, apiClient, mainParams)

	// Start Server Instance separate from Controller
	if err := serverInstance.Start(); err != nil {
		t.Fatalf("Failed to start server instance: %v", err)
	}
	defer serverInstance.Close()

	err = c.Start()
	if err != nil {
		t.Fatalf("Failed to start controller: %v", err)
	}
	defer c.Close()

	// 5. Setup Local VLESS Client to generate traffic
	clientPort := uint32(getFreePortVal())
	clientInboundDetour := &conf.InboundDetourConfig{
		Tag:      "dokodemo",
		Protocol: "dokodemo-door",
		ListenOn: &conf.Address{Address: xraynet.ParseAddress("127.0.0.1")},
		PortList: &conf.PortList{Range: []conf.PortRange{{From: clientPort, To: clientPort}}},
		Settings: nil, // will set below
	}
	dokodemoSettings := fmt.Sprintf(`{"address": "127.0.0.1", "port": %d, "network": "tcp"}`, portFastInt)
	dokoRaw := json.RawMessage(dokodemoSettings)
	clientInboundDetour.Settings = &dokoRaw
	clientInbound, _ := clientInboundDetour.Build()

	clientOutboundDetour := &conf.OutboundDetourConfig{
		Protocol: "vless",
		Settings: nil,
	}
	vlessSettings := fmt.Sprintf(`{
		"vnext": [{
			"address": "127.0.0.1",
			"port": %d,
			"users": [{"id": "b831381d-6324-4d53-ad4f-8cda48b30811", "encryption": "none"}]
		}]
	}`, serverPort)
	vlessRaw := json.RawMessage(vlessSettings)
	clientOutboundDetour.Settings = &vlessRaw
	clientOutbound, _ := clientOutboundDetour.Build()

	clientAccessLogPath := filepath.Join(tempDir, "client_access.log")
	clientErrorLogPath := filepath.Join(tempDir, "client_error.log")

	clientConfig := &core.Config{
		App: []*serial.TypedMessage{
			serial.ToTypedMessage(&log.Config{
				ErrorLogType:  log.LogType_File,
				ErrorLogPath:  clientErrorLogPath,
				ErrorLogLevel: comlog.Severity_Debug,
				AccessLogType: log.LogType_File,
				AccessLogPath: clientAccessLogPath,
			}),
			serial.ToTypedMessage(&dispatcher.Config{}),
			serial.ToTypedMessage(&proxyman.InboundConfig{}),
			serial.ToTypedMessage(&proxyman.OutboundConfig{}),
		},
		Inbound:  []*core.InboundHandlerConfig{clientInbound},
		Outbound: []*core.OutboundHandlerConfig{clientOutbound},
	}

	defer func() {
		if t.Failed() {
			t.Logf("Server Access Log: %s", accessLogPath)
			content, _ := os.ReadFile(accessLogPath)
			t.Log(string(content))
			t.Logf("Server Error Log: %s", errorLogPath)
			errContent, _ := os.ReadFile(errorLogPath)
			t.Log(string(errContent))

			t.Logf("Client Access Log: %s", clientAccessLogPath)
			cContent, _ := os.ReadFile(clientAccessLogPath)
			t.Log(string(cContent))
			t.Logf("Client Error Log: %s", clientErrorLogPath)
			cErrContent, _ := os.ReadFile(clientErrorLogPath)
			t.Log(string(cErrContent))
		}
	}()

	clientInstance, err := core.New(clientConfig)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	if err := clientInstance.Start(); err != nil {
		t.Fatalf("Failed to start client: %v", err)
	}
	defer clientInstance.Close()

	// 6. Wait for Observatory Probe
	t.Log("Waiting for Observatory to probe...")
	time.Sleep(5 * time.Second)

	// 7. Send Request via Client
	conn, err := stdnet.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", clientPort))
	if err != nil {
		t.Fatalf("Failed to connect to client: %v", err)
	}
	defer conn.Close()

	// Prepare HTTP request
	req := fmt.Sprintf("GET / HTTP/1.1\r\nHost: 127.0.0.1:%d\r\n\r\n", portFastInt)
	conn.Write([]byte(req))

	// Read response
	buf := make([]byte, 1024)
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatalf("Read failed: %v", err)
	}
	response := string(buf[:n])

	if !strings.Contains(response, "FAST") {
		t.Logf("Response: %s", response)
		t.Errorf("Expected 'FAST' response, getting something else (load balancing failed?)")
	} else {
		t.Log("Success: Received 'FAST' response, traffic routed to healthy backend.")
	}
	t.Log("Test logic finished.")
}
