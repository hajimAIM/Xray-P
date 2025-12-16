package controller_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"runtime"
	"testing"
	"time"

	"github.com/xtls/xray-core/app/dispatcher"
	"github.com/xtls/xray-core/app/proxyman"
	"github.com/xtls/xray-core/app/stats"
	"github.com/xtls/xray-core/common/serial"
	"github.com/xtls/xray-core/core"

	"Xray-P/api"
	"Xray-P/api/sspanel"
	_ "Xray-P/cmd/distro/all"
	"Xray-P/common/mylego"
	. "Xray-P/service/controller"
)

func TestController(t *testing.T) {
	config := &core.Config{
		App: []*serial.TypedMessage{
			serial.ToTypedMessage(&dispatcher.Config{}),
			serial.ToTypedMessage(&proxyman.InboundConfig{}),
			serial.ToTypedMessage(&proxyman.OutboundConfig{}),
			serial.ToTypedMessage(&stats.Config{}),
		}}

	server, err := core.New(config)
	if err != nil {
		t.Errorf("failed to create instance: %s", err)
		return
	}
	defer server.Close()
	if err = server.Start(); err != nil {
		t.Errorf("Failed to start instance: %s", err)
	}
	certConfig := &mylego.CertConfig{
		CertMode:   "none",
		CertDomain: "test.ss.tk",
		Provider:   "alidns",
		Email:      "ss@ss.com",
	}
	controlerConfig := &Config{
		UpdatePeriodic: 5,
		CertConfig:     certConfig,
	}
	// Create Mock Server
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/mod_mu/nodes/41/info" || (len(r.URL.Path) > 14 && r.URL.Path[:14] == "/mod_mu/nodes/" && r.URL.Path[len(r.URL.Path)-5:] == "/info") {
			// Return Mock Node Info
			// Use simple SS response for testing
			nodeInfo := sspanel.NodeInfoResponse{
				RawServerString: "127.0.0.1;12345;2;tls;ws;server=127.0.0.1|host=test.com",
				CustomConfig:    json.RawMessage(`{"offset_port_node": "12345"}`),
				Type:            "1",
			}
			nodeInfoBytes, _ := json.Marshal(nodeInfo)

			json.NewEncoder(w).Encode(sspanel.Response{
				Ret:  1,
				Data: json.RawMessage(nodeInfoBytes),
			})
			return
		}
		if r.URL.Path == "/mod_mu/users" {
			// Return Mock User List
			json.NewEncoder(w).Encode(struct {
				Ret  int                    `json:"ret"`
				Data []sspanel.UserResponse `json:"data"`
			}{
				Ret:  1,
				Data: []sspanel.UserResponse{},
			})
			return
		}
		// Default success for reporting endpoints
		json.NewEncoder(w).Encode(struct {
			Ret int `json:"ret"`
		}{Ret: 1})
	}))
	defer ts.Close()

	apiConfig := &api.Config{
		APIHost:  ts.URL,
		Key:      "123",
		NodeID:   41,
		NodeType: "V2ray",
	}
	apiClient := sspanel.New(apiConfig)
	c := New(server, apiClient, controlerConfig)
	fmt.Println("Mock Server URL:", ts.URL)

	// Run controller in a goroutine or just start/stop for test
	// Since c.Start() blocks in main loop (it has periodic tasks), we might want to test specific methods or run it briefly.
	// However, looking at source, c.Start() starts goroutines and returns nil unless error.

	err = c.Start()
	if err != nil {
		t.Error(err)
	}

	// Let it run for a bit
	time.Sleep(2 * time.Second)

	// Explicitly triggering GC to remove garbage from config loading.
	runtime.GC()

	// Handle signals if we want to wait, or just finish
	// For automated test, we usually don't want to block forever on signals unless running interactively.
	// We'll remove the blocking signal wait for this test execution to allow it to pass/fail.
	// If you want to keep the server running for manual testing, uncomment the signal part.
	/*
		{
			osSignals := make(chan os.Signal, 1)
			signal.Notify(osSignals, os.Interrupt, syscall.SIGTERM)
			<-osSignals
		}
	*/
}
