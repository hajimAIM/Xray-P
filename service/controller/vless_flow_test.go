package controller_test

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"runtime/debug"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/xtls/xray-core/app/dispatcher"
	"github.com/xtls/xray-core/app/proxyman"
	xnet "github.com/xtls/xray-core/common/net"
	"github.com/xtls/xray-core/common/serial"
	"github.com/xtls/xray-core/core"
	"github.com/xtls/xray-core/infra/conf"
	"github.com/xtls/xray-core/proxy/dokodemo"
	"github.com/xtls/xray-core/proxy/freedom"
	"github.com/xtls/xray-core/transport/internet"

	// Register transports and app implementations
	_ "github.com/xtls/xray-core/app/dispatcher"
	_ "github.com/xtls/xray-core/app/proxyman/inbound"
	_ "github.com/xtls/xray-core/app/proxyman/outbound"
	_ "github.com/xtls/xray-core/transport/internet/tcp"
	_ "github.com/xtls/xray-core/transport/internet/websocket"

	// Register proxy implementations
	_ "github.com/xtls/xray-core/proxy/dokodemo"
	_ "github.com/xtls/xray-core/proxy/freedom"
	_ "github.com/xtls/xray-core/proxy/vless/inbound"
	_ "github.com/xtls/xray-core/proxy/vless/outbound"

	"Xray-P/api"
	"Xray-P/service/controller"
)

func TestVlessFlowSanitization(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("SUCCESS"))
	}))
	defer backend.Close()

	mockAPI := &MockAPIClient{
		NodeInfo: &api.NodeInfo{
			NodeType:          "Vless",
			Port:              getFreePort(),
			TransportProtocol: "ws",
			EnableVless:       true,
			VlessFlow:         "xtls-rprx-vision", // INVALID for WS! Should be cleared by fix.
			CypherMethod:      "none",
			ServerKey:         "server_key",
		},
		Users: []api.UserInfo{
			{
				UID:    1,
				UUID:   "068e3900-3333-4444-5555-666677778888",
				Email:  "test@example.com",
				Passwd: "password",
			},
		},
	}

	cfg := &controller.Config{
		UpdatePeriodic: 60,
		ListenIP:       "127.0.0.1",
	}

	serverConfig := &core.Config{
		App: []*serial.TypedMessage{
			serial.ToTypedMessage(&dispatcher.Config{}),
			serial.ToTypedMessage(&proxyman.InboundConfig{}),
			serial.ToTypedMessage(&proxyman.OutboundConfig{}),
		},
		Outbound: []*core.OutboundHandlerConfig{
			{
				Tag: "direct",
				ProxySettings: serial.ToTypedMessage(&freedom.Config{
					DomainStrategy: internet.DomainStrategy_USE_IP,
				}),
			},
		},
	}

	server, err := core.New(serverConfig)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}

	ctrl := controller.New(server, mockAPI, cfg)

	err = server.Start()
	assert.NoError(t, err)
	defer server.Close()

	// Run capturing panic
	go func() {
		defer func() {
			if r := recover(); r != nil {
				fmt.Printf("PANIC IN CONTROLLER START: %v\nStack: %s", r, string(debug.Stack()))
			}
		}()
		if err := ctrl.Start(); err != nil {
			fmt.Printf("Controller Start Error: %v", err)
		}
	}()

	time.Sleep(2 * time.Second)

	clientPort := getFreePort()

	// Build VLESS Outbound Config using conf helper and JSON RawMessage for User
	userJSON := fmt.Sprintf(`{"id": "%s", "flow": "", "encryption": "none", "level": 0}`, mockAPI.Users[0].UUID)

	vlessOutConf := &conf.VLessOutboundConfig{
		Vnext: []*conf.VLessOutboundVnext{
			{
				Address: &conf.Address{Address: xnet.ParseAddress("127.0.0.1")},
				Port:    uint16(mockAPI.NodeInfo.Port),
				Users: []json.RawMessage{
					json.RawMessage(userJSON),
				},
			},
		},
	}
	vlessOutProto, err := vlessOutConf.Build()
	if err != nil {
		t.Fatalf("Failed to build vless outbound config: %v", err)
	}

	// Build Stream Settings using conf helper
	wsProto := conf.TransportProtocol("ws")
	streamConf := &conf.StreamConfig{
		Network: &wsProto,
		WSSettings: &conf.WebSocketConfig{
			Path: "/",
		},
	}
	streamProto, err := streamConf.Build()
	if err != nil {
		t.Fatalf("Failed to build stream config: %v", err)
	}

	clientConfig := &core.Config{
		App: []*serial.TypedMessage{
			serial.ToTypedMessage(&dispatcher.Config{}),
			serial.ToTypedMessage(&proxyman.InboundConfig{}),
			serial.ToTypedMessage(&proxyman.OutboundConfig{}),
		},
		Inbound: []*core.InboundHandlerConfig{
			{
				Tag: "client_in",
				ReceiverSettings: serial.ToTypedMessage(&proxyman.ReceiverConfig{
					PortList: &xnet.PortList{
						Range: []*xnet.PortRange{
							{From: uint32(clientPort), To: uint32(clientPort)},
						},
					},
					Listen: &xnet.IPOrDomain{Address: &xnet.IPOrDomain_Ip{Ip: []byte{127, 0, 0, 1}}},
				}),
				ProxySettings: serial.ToTypedMessage(&dokodemo.Config{
					Address:  &xnet.IPOrDomain{Address: &xnet.IPOrDomain_Ip{Ip: []byte{127, 0, 0, 1}}},
					Port:     uint32(backend.Listener.Addr().(*net.TCPAddr).Port),
					Networks: []xnet.Network{xnet.Network_TCP},
				}),
			},
		},
		Outbound: []*core.OutboundHandlerConfig{
			{
				ProxySettings: serial.ToTypedMessage(vlessOutProto),
				SenderSettings: serial.ToTypedMessage(&proxyman.SenderConfig{
					StreamSettings: streamProto,
				}),
			},
		},
	}

	clientCore, err := core.New(clientConfig)
	if err != nil {
		t.Fatalf("Failed to create client core: %v", err)
	}
	err = clientCore.Start()
	assert.NoError(t, err)
	defer clientCore.Close()

	reqUrl := fmt.Sprintf("http://127.0.0.1:%d", clientPort)
	resp, err := http.Get(reqUrl)

	if err != nil {
		t.Fatalf("HTTP Get failed: %v", err)
	}
	defer resp.Body.Close()

	assert.Equal(t, 200, resp.StatusCode)
}

func getFreePort() uint32 {
	addr, err := net.ResolveTCPAddr("tcp", "localhost:0")
	if err != nil {
		return 0
	}
	l, err := net.ListenTCP("tcp", addr)
	if err != nil {
		return 0
	}
	defer l.Close()
	return uint32(l.Addr().(*net.TCPAddr).Port)
}

type MockAPIClient struct {
	NodeInfo *api.NodeInfo
	Users    []api.UserInfo
}

func (m *MockAPIClient) GetNodeInfo() (*api.NodeInfo, error)                            { return m.NodeInfo, nil }
func (m *MockAPIClient) GetUserList() (*[]api.UserInfo, error)                          { return &m.Users, nil }
func (m *MockAPIClient) ReportNodeStatus(nodeStatus *api.NodeStatus) error              { return nil }
func (m *MockAPIClient) ReportNodeOnlineUsers(onlineUserList *[]api.OnlineUser) error   { return nil }
func (m *MockAPIClient) ReportUserTraffic(userTraffic *[]api.UserTraffic) (err error)   { return nil }
func (m *MockAPIClient) Describe() api.ClientInfo                                       { return api.ClientInfo{} }
func (m *MockAPIClient) GetNodeRule() (ruleList *[]api.DetectRule, err error)           { return nil, nil }
func (m *MockAPIClient) ReportIllegal(detectResultList *[]api.DetectResult) (err error) { return nil }
func (m *MockAPIClient) Debug()                                                         {}
