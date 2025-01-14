package mammotion

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha1"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"mammo/aliyuniot"
	"os"
	"sync"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

type MammotionMQTT struct {
	RegionID       string
	ProductKey     string
	DeviceName     string
	DeviceSecret   string
	IotToken       string
	CloudClient    *aliyuniot.CloudIOTGateway
	ClientID       string
	MQTTClientID   string
	MQTTUsername   string
	MQTTPassword   string
	MQTTClient     mqtt.Client
	IsConnected    bool
	IsReady        bool
	OnConnected    func()
	OnReady        func()
	OnError        func(string)
	OnDisconnected func()
	OnMessage      func(topic string, payload []byte, iotID string)
	mu             sync.Mutex
}

// https://www.alibabacloud.com/help/en/iot/use-cases/use-the-paho-mqtt-library-for-go-to-connect-a-device-to-iot-platform
func NewMammotionMQTT(regionID, productKey, deviceName, deviceSecret, iotToken string, cloudClient *aliyuniot.CloudIOTGateway) *MammotionMQTT {
    clientID := cloudClient.ClientID
	if clientID == "" {
		clientID = fmt.Sprintf("golang-%s", deviceName)
	}

    var raw_broker bytes.Buffer
    raw_broker.WriteString("tls://")
    raw_broker.WriteString(productKey)
    raw_broker.WriteString(".iot-as-mqtt.cn-shanghai.aliyuncs.com:1883")
    opts := mqtt.NewClientOptions().AddBroker(raw_broker.String());

    auth := calculate_sign(clientID, productKey, deviceName, deviceSecret)
    opts.SetClientID(auth.mqttClientId)
    opts.SetUsername(auth.username)
    opts.SetPassword(auth.password)
    opts.SetKeepAlive(60 * 2 * time.Second)
    opts.SetDefaultPublishHandler(f)

    /*
    fmt.Println("MQTT Client Options:")
    fmt.Println("  Broker:", opts.Servers)
    fmt.Println("  Client ID:", opts.ClientID)
    fmt.Println("  Username:", opts.Username)
    fmt.Println("  Password:", opts.Password)
    fmt.Println("  Protocol Version:", opts.ProtocolVersion)
    fmt.Println("  clientID:", clientID)
    fmt.Println("  productKey:", productKey)
    fmt.Println("  deviceName:", deviceName)
    fmt.Println("  deviceSecret:", deviceSecret)
    */
    tlsconfig := NewTLSConfig()
    opts.SetTLSConfig(tlsconfig)

    mqtt.ERROR = log.New(os.Stdout, "[ERROR] ", 0)
	mqtt.CRITICAL = log.New(os.Stdout, "[CRIT] ", 0)
	mqtt.WARN = log.New(os.Stdout, "[WARN]  ", 0)
	mqtt.DEBUG = log.New(os.Stdout, "[DEBUG] ", 0)

	return &MammotionMQTT{
		RegionID:     regionID,
		ProductKey:   productKey,
		DeviceName:   deviceName,
		DeviceSecret: deviceSecret,
		IotToken:     iotToken,
		CloudClient:  cloudClient,
		ClientID:     clientID,
		MQTTClientID: auth.mqttClientId,
		MQTTUsername: auth.username,
		MQTTPassword: auth.password,
		MQTTClient:   mqtt.NewClient(opts),
	}
}

func (m *MammotionMQTT) ConnectAsync() {

    if (m.MQTTClient.IsConnected()) {
        return
    }
	log.Println("Connecting...")
	if token := m.MQTTClient.Connect(); token.Wait() && token.Error() != nil {
        log.Fatal(fmt.Sprintf("Connection error: %s", token.Error()))
	}
    println("MQTT Connected")
}

func (m *MammotionMQTT) Disconnect() {
	log.Println("Disconnecting...")
	m.MQTTClient.Disconnect(250)
}

func (m *MammotionMQTT) Subscribe(topic string, qos byte, callback mqtt.MessageHandler) {
	if token := m.MQTTClient.Subscribe(topic, qos, callback); token.Wait() && token.Error() != nil {
		log.Fatal(token.Error())
	}
}

func (m *MammotionMQTT) Publish(topic string, payload interface{}) {
	data, err := json.Marshal(payload)
	if err != nil {
		log.Fatal(err)
	}
	if token := m.MQTTClient.Publish(topic, 0, false, data); token.Wait() && token.Error() != nil {
		log.Fatal(token.Error())
	}
}

func (m *MammotionMQTT) OnThingEnable(client mqtt.Client, msg mqtt.Message) {
	log.Println("Thing enabled")
	m.IsConnected = true
	m.Subscribe(fmt.Sprintf("/sys/%s/%s/app/down/account/bind_reply", m.ProductKey, m.DeviceName), 0, m.OnMessageReceived)
	m.Subscribe(fmt.Sprintf("/sys/%s/%s/app/down/thing/event/property/post_reply", m.ProductKey, m.DeviceName), 0, m.OnMessageReceived)
	m.Subscribe(fmt.Sprintf("/sys/%s/%s/app/down/thing/wifi/status/notify", m.ProductKey, m.DeviceName), 0, m.OnMessageReceived)
	m.Subscribe(fmt.Sprintf("/sys/%s/%s/app/down/thing/wifi/connect/event/notify", m.ProductKey, m.DeviceName), 0, m.OnMessageReceived)
	m.Subscribe(fmt.Sprintf("/sys/%s/%s/app/down/_thing/event/notify", m.ProductKey, m.DeviceName), 0, m.OnMessageReceived)
	m.Subscribe(fmt.Sprintf("/sys/%s/%s/app/down/thing/events", m.ProductKey, m.DeviceName), 0, m.OnMessageReceived)
	m.Subscribe(fmt.Sprintf("/sys/%s/%s/app/down/thing/status", m.ProductKey, m.DeviceName), 0, m.OnMessageReceived)
	m.Subscribe(fmt.Sprintf("/sys/%s/%s/app/down/thing/properties", m.ProductKey, m.DeviceName), 0, m.OnMessageReceived)
	m.Subscribe(fmt.Sprintf("/sys/%s/%s/app/down/thing/model/down_raw", m.ProductKey, m.DeviceName), 0, m.OnMessageReceived)

	m.Publish(fmt.Sprintf("/sys/%s/%s/app/up/account/bind", m.ProductKey, m.DeviceName), map[string]interface{}{
		"id":      "msgid1",
		"version": "1.0",
		"request": map[string]string{
			"clientId": m.MQTTUsername,
		},
		"params": map[string]string{
			"iotToken": m.IotToken,
		},
	})

	if m.OnReady != nil {
		m.IsReady = true
		m.OnReady()
	}
}

func (m *MammotionMQTT) OnMessageReceived(client mqtt.Client, msg mqtt.Message) {
	log.Printf("Message received on topic %s: %s", msg.Topic(), string(msg.Payload()))
	var payload map[string]interface{}
	if err := json.Unmarshal(msg.Payload(), &payload); err != nil {
		log.Println("Error unmarshalling payload:", err)
		return
	}
	iotID := payload["params"].(map[string]interface{})["iotId"].(string)
	if iotID != "" && m.OnMessage != nil {
		m.OnMessage(msg.Topic(), msg.Payload(), iotID)
	}
}

func (m *MammotionMQTT) OnConnect(client mqtt.Client) {
	m.IsConnected = true
	if m.OnConnected != nil {
		m.OnConnected()
	}
	log.Println("Connected")
}

func (m *MammotionMQTT) OnDisconnect(client mqtt.Client, err error) {
	log.Println("Disconnected")
	m.IsConnected = false
	m.IsReady = false
	if m.OnDisconnected != nil {
		m.OnDisconnected()
	}
}

func (m *MammotionMQTT) GetCloudClient() *aliyuniot.CloudIOTGateway {
	return m.CloudClient
}

type AuthInfo struct {
    password, username, mqttClientId string;
}

func calculate_sign(clientId, productKey, deviceName, deviceSecret string) AuthInfo {

    timeStamp := fmt.Sprintf("%d", time.Now().Unix())
    var raw_passwd bytes.Buffer
    raw_passwd.WriteString("clientId" + fmt.Sprintf("%s&%s", productKey, deviceName))
    raw_passwd.WriteString("deviceName")
    raw_passwd.WriteString(deviceName)
    raw_passwd.WriteString("productKey")
    raw_passwd.WriteString(productKey);
    raw_passwd.WriteString("timestamp")
    raw_passwd.WriteString(timeStamp)

    fmt.Println("Raw password before SHA1: ", raw_passwd.String())
    mac := hmac.New(sha1.New, []byte(deviceSecret))
    mac.Write([]byte(raw_passwd.String()))
    password := fmt.Sprintf("%02x", mac.Sum(nil))
    username := deviceName + "&" + productKey;

    var MQTTClientId bytes.Buffer
    MQTTClientId.WriteString(fmt.Sprintf("%s&%s", productKey, deviceName))
    // hmac, use sha1; securemode=2 means TLS connection 
    MQTTClientId.WriteString("|securemode=2,signmethod=hmacsha1,timestamp=")
    MQTTClientId.WriteString(timeStamp)
    MQTTClientId.WriteString("|")

    auth := AuthInfo{password:password, username:username, mqttClientId:MQTTClientId.String()}
    return auth;
}

func NewTLSConfig() *tls.Config {
    // Import trusted certificates from CAfile.pem.
    // Alternatively, manually add CA certificates to default openssl CA bundle.
    certpool := x509.NewCertPool()
    pemCerts, err := ioutil.ReadFile("./x509/root.pem")
    if err != nil {
        fmt.Println("0. read file error, game over!!")
        panic(err)
    }

    if ok := certpool.AppendCertsFromPEM([]byte(pemCerts)); !ok {
		fmt.Println("failed to parse root certificate")
		panic(err)
	}

    // Create tls.Config with desired tls properties
    return &tls.Config{
        // RootCAs = certs used to verify server cert.
        RootCAs: certpool,
        // ClientAuth = whether to request cert from server.
        // Since the server is set up for SSL, this happens
        // anyways.
        ClientAuth: tls.NoClientCert,
        // ClientCAs = certs used to validate client cert.
        ClientCAs: nil,
        // InsecureSkipVerify = verify that cert contents
        // match server. IP matches what is in cert etc.
        InsecureSkipVerify: false,
        // Certificates = list of certs client sends to server.
        // Certificates: []tls.Certificate{cert},
    }
}


var f mqtt.MessageHandler = func(client mqtt.Client, msg mqtt.Message) {
    fmt.Printf("TOPIC: %s\n", msg.Topic())
    fmt.Printf("MSG: %s\n", msg.Payload())
}
