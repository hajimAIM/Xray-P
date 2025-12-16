# Xray-P

**Xray-P** is a high-performance Xray backend framework, customized for **SSPanel-UIM**.
This project is based on [XrayR-project/XrayR](https://github.com/XrayR-project/XrayR) and has been modernized to support the latest features of [Xray-core](https://github.com/xtls/xray-core).

## 🚀 Features

- **Base Architecture**: Derived from the robust XrayR framework.
- **Latest Core**: Built upon **Xray-core v25.10.15**.
- **Observatory Integration**: Supports Xray's `Observatory` for advanced outbound latency probing and load balancing.
- **Custom Dispatcher**: Includes a custom dispatcher (forked from Xray-core) to provide precise user traffic tracking and speed limiting functionalities required by panel implementations.
- **VLESS Compatibility**: Fixes connectivity issues with VLESS-WS flows (automatically sanitizes invalid flow settings).

## ⚠️ Supported Platforms

Currently, this project **ONLY** supports the following panel:

- [SSPanel-UIM](https://github.com/Anankke/SSPanel-UIM)

*Other panels (v2board, PMPanel, Proxypanel) are NOT supported in this version.*

## 🛠️ Installation & Usage

### Prerequisites
- Go 1.23+ (Recommended)
- Basic knowledge of SSPanel-UIM configuration.

### Build from Source

```powershell
# Clone the repository
git clone https://github.com/hajimAIM/Xray-P.git
cd Xray-P

# Build the binary
go build .
```

### Configuration

1. Copy the example configuration file:
   ```powershell
   Copy-Item release/config/config.yml.example config.yml
   ```

2. Edit `config.yml` to match your SSPanel-UIM settings.

   **Example Configuration:**
   ```yaml
   Log:
     Level: warning # Log level: none, error, warning, info, debug 
     AccessPath: # /etc/XrayR/access.Log
     ErrorPath: # /etc/XrayR/error.log
   DnsConfigPath: # /etc/XrayR/dns.json
   RouteConfigPath: # /etc/XrayR/route.json
   InboundConfigPath: # /etc/XrayR/custom_inbound.json
   OutboundConfigPath: # /etc/XrayR/custom_outbound.json
   ObservatoryConfigPath: # /etc/XrayR/observatory.json
   ConnetionConfig:
     Handshake: 4 # Handshake time limit, Second
     ConnIdle: 30 # Connection idle time limit, Second
     UplinkOnly: 2 # Time limit when the connection downstream is closed, Second
     DownlinkOnly: 4 # Time limit when the connection upstream is closed, Second
     BufferSize: 64 # The internal buffer size of each connection, kB
   Nodes:
     -
       PanelType: "SSPanel" # Support SSPanel
       ApiConfig:
         ApiHost: "https://your-panel-domain.com"
         ApiKey: "your-node-api-key"
         NodeID: 41
         NodeType: V2ray # Node type: V2ray, Shadowsocks, Trojan, Shadowsocks-Plugin
         Timeout: 30 # Timeout for the api request
         EnableVless: false # Enable Vless for V2ray Type
         EnableXTLS: false # Enable XTLS for V2ray and Trojan
         SpeedLimit: 0 # Mbps, Local settings will replace remote settings, 0 means disable
         DeviceLimit: 0 # Local settings will replace remote settings, 0 means disable
         RuleListPath: # /etc/XrayR/rulelist
     # ... more nodes
   ```

### Running

Run the application directly:

```powershell
./Xray-P.exe --config config.yml
```

## 📜 License

This project is licensed under the same terms as the original XrayR project.
