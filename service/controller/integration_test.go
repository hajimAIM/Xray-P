package controller_test

import (
	"encoding/json"
	"fmt"
	stdnet "net"
	"net/http"
	"net/http/httptest"
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

	"Xray-P/api"
	"Xray-P/api/sspanel"
	_ "Xray-P/cmd/distro/all"
	"Xray-P/common/mylego"
	. "Xray-P/service/controller"
)

func TestVlessIntegration(t *testing.T) {
	reportChan := make(chan string, 1)

	// 1. Setup Mock SSPanel Server for VLESS
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if r.URL.Path == "/mod_mu/users/aliveip" {
			reportChan <- "127.0.0.1"
			json.NewEncoder(w).Encode(struct {
				Ret int `json:"ret"`
			}{Ret: 1})
			return
		}

		if r.URL.Path == "/mod_mu/nodes/42/info" || (len(r.URL.Path) > 14 && r.URL.Path[:14] == "/mod_mu/nodes/" && r.URL.Path[len(r.URL.Path)-5:] == "/info") {
			nodeInfo := sspanel.NodeInfoResponse{
				RawServerString: "127.0.0.1;12346;0;none;tcp;server=127.0.0.1|host=vless.test.com",
				// Enable VLESS
				CustomConfig: json.RawMessage(`{"offset_port_node": "12346", "enable_vless": "1", "vless_flow": "", "network": "tcp"}`),
				Type:         "1",
				Version:      "2021.11",
			}
			nodeInfoBytes, _ := json.Marshal(nodeInfo)
			json.NewEncoder(w).Encode(sspanel.Response{
				Ret:  1,
				Data: json.RawMessage(nodeInfoBytes),
			})
			return
		}

		if r.URL.Path == "/mod_mu/users" {
			users := []sspanel.UserResponse{
				{
					ID:          1001,
					UUID:        "b831381d-6324-4d53-ad4f-8cda48b30811",
					Passwd:      "password", // Ignored for VLESS
					Port:        12346,
					Method:      "aes-128-gcm",
					SpeedLimit:  0,
					DeviceLimit: 0,
					AliveIP:     0,
				},
			}
			usersBytes, _ := json.Marshal(users)
			json.NewEncoder(w).Encode(sspanel.Response{
				Ret:  1,
				Data: json.RawMessage(usersBytes),
			})
			return
		}

		json.NewEncoder(w).Encode(struct {
			Ret int `json:"ret"`
		}{Ret: 1})
	}))
	defer ts.Close()

	// 2. Setup Xray Config for Server
	logConfig := &log.Config{
		ErrorLogType:  log.LogType_Console,
		ErrorLogLevel: comlog.Severity_Debug,
	}
	serverConfig := &core.Config{
		App: []*serial.TypedMessage{
			serial.ToTypedMessage(logConfig),
			serial.ToTypedMessage(&dispatcher.Config{}),
			serial.ToTypedMessage(&proxyman.InboundConfig{}),
			serial.ToTypedMessage(&proxyman.OutboundConfig{}),
			serial.ToTypedMessage(&stats.Config{}),
		},
	}
	server, err := core.New(serverConfig)
	if err != nil {
		t.Fatalf("Failed to create server instance: %v", err)
	}
	defer server.Close()
	if err := server.Start(); err != nil {
		t.Fatalf("Failed to start server instance: %v", err)
	}

	// 3. Setup Controller Config
	certConfig := &mylego.CertConfig{
		CertMode: "none",
	}
	controlerConfig := &Config{
		UpdatePeriodic: 5,
		CertConfig:     certConfig,
		ListenIP:       "0.0.0.0",
		EnableREALITY:  false,
	}

	apiConfig := &api.Config{
		APIHost:  ts.URL,
		Key:      "123",
		NodeID:   42,
		NodeType: "V2ray",
	}

	apiClient := sspanel.New(apiConfig)
	c := New(server, apiClient, controlerConfig)

	err = c.Start()
	if err != nil {
		t.Fatalf("Failed to start controller: %v", err)
	}

	// 5. Setup Xray Client (VLESS)
	clientPort := uint32(20000)

	// Build Client Inbound (Dokodemo)
	dokodemoSettings := map[string]interface{}{
		"address": "127.0.0.1",
		"port":    54321,
		"network": "tcp",
	}
	dokodemoBytes, _ := json.Marshal(dokodemoSettings)
	clientInboundDetour := &conf.InboundDetourConfig{
		Tag:      "dokodemo",
		Protocol: "dokodemo-door",
		ListenOn: &conf.Address{Address: xraynet.ParseAddress("127.0.0.1")},
		PortList: &conf.PortList{Range: []conf.PortRange{{From: clientPort, To: clientPort}}},
		Settings: (*json.RawMessage)(&dokodemoBytes),
	}
	clientInbound, err := clientInboundDetour.Build()
	if err != nil {
		t.Fatalf("Failed to build client inbound: %v", err)
	}

	// Build Client Outbound (VLESS)
	vlessSettings := map[string]interface{}{
		"vnext": []map[string]interface{}{
			{
				"address": "127.0.0.1",
				"port":    12346,
				"users": []map[string]interface{}{
					{
						"id":         "b831381d-6324-4d53-ad4f-8cda48b30811",
						"encryption": "none",
						"flow":       "",
					},
				},
			},
		},
	}
	vlessBytes, _ := json.Marshal(vlessSettings)
	clientOutboundDetour := &conf.OutboundDetourConfig{
		Protocol: "vless",
		Settings: (*json.RawMessage)(&vlessBytes),
	}
	clientOutbound, err := clientOutboundDetour.Build()
	if err != nil {
		t.Fatalf("Failed to build client outbound: %v", err)
	}

	clientConfig := &core.Config{
		App: []*serial.TypedMessage{
			serial.ToTypedMessage(&dispatcher.Config{}),
			serial.ToTypedMessage(&proxyman.InboundConfig{}),
			serial.ToTypedMessage(&proxyman.OutboundConfig{}),
		},
		Inbound:  []*core.InboundHandlerConfig{clientInbound},
		Outbound: []*core.OutboundHandlerConfig{clientOutbound},
	}

	clientInstance, err := core.New(clientConfig)
	if err != nil {
		t.Fatalf("Failed to create client instance: %v", err)
	}
	if err := clientInstance.Start(); err != nil {
		t.Fatalf("Failed to start client instance: %v", err)
	}
	defer clientInstance.Close()

	// 6. Generate Traffic
	time.Sleep(1 * time.Second)
	conn, err := stdnet.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", clientPort), 1*time.Second)
	if err != nil {
		t.Fatalf("Failed to connect to client port: %v", err)
	}
	conn.Write([]byte("ping"))
	conn.Close()

	// 7. Verify IP Reporting
	select {
	case reportedIP := <-reportChan:
		t.Logf("Received IP Report: %s", reportedIP)
	case <-time.After(8 * time.Second):
		t.Errorf("Timeout waiting for IP report. Controller should have reported online user.")
	}
}
