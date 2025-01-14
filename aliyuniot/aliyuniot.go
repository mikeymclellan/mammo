package aliyuniot

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
    iot "github.com/alibabacloud-go/iot-api-gateway/client"
    util "github.com/alibabacloud-go/tea-utils/service"
	"github.com/alibabacloud-go/tea/tea"
)

const (
    APP_KEY = "34231230"
    APP_SECRET = "1ba85698bb10e19c6437413b61ba3445"
    APP_VERSION = "1.11.130"
    ALIYUN_DOMAIN = "api.link.aliyun.com"
)

type CloudIOTGateway struct {
	AppKey                   string
	AppSecret                string
	Domain                   string
	ClientID                 string
	DeviceSN                 string
	Utdid                    string
	ConnectResponse          *ConnectResponse
	LoginByOAuthResponse     *LoginByOAuthResponse
	AepResponse              *AepResponse
	SessionByAuthCodeResponse *SessionByAuthCodeResponse
	RegionResponse           *RegionResponse
	DevicesByAccountResponse *ListingDevByAccountResponse
	IotTokenIssuedAt         int64
}

type SendCloudCommandParams struct {
	Args       map[string]string `json:"args"`
	Identifier string            `json:"identifier"`
	IotId      string            `json:"iotId"`
}

type SendCloudCommandResponse struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type ConnectResponse struct {
	Data struct {
		Vid string `json:"vid"`
        Data struct {
            Device struct {
                Data struct {
                    DeviceId string `json:"deviceId"`
                } `json:"data"`
            } `json:"device"`
        } `json:"data"`
	} `json:"data"`
}

type LoginByOAuthResponse struct {
    Api string `json:"api"`
    Data struct {
        Code int `json:"code"`
        Data struct {
            LoginSuccessResult struct {
                InitPwd interface{} `json:"initPwd"`
                OauthOtherInfo struct {
                    SidExpiredTime float64 `json:"SidExpiredTime"`
                } `json:"oauthOtherInfo"`
                OpenAccount struct {
                    AvatarUrl string `json:"avatarUrl"`
                    Country string `json:"country"`
                    DisplayName string `json:"displayName"`
                    DomainId float64 `json:"domainId"`
                    //EnableDevice bool `json:"enableDevice"`
                    //HasPassword bool `json:"hasPassword"`
                    Id float64 `json:"id"`
                    //MobileConflictAccount bool `json:"mobileConflictAccount"`
                    //MobileLocationCode float64 `json:"mobileLocationCode"`
                    OpenId string `json:"openId"`
                    //PwdVersion float64 `json:"pwdVersion"`
                    Status float64 `json:"status"`
                    //SubAccount bool `json:"subAccount"`
                } `json:"openAccount"`
                ReTokenExpireIn float64 `json:"reTokenExpireIn"`
                RefreshToken string `json:"refreshToken"`
                Sid string `json:"sid"`
                SidExpireIn float64 `json:"sidExpireIn"`
                Token string `json:"token"`
                UidToken interface{} `json:"uidToken"`
            } `json:"loginSuccessResult"`
            //MobileBindRequired bool `json:"mobileBindRequired"`
        } `json:"data"`
        Message string `json:"message"`
        SubCode int `json:"subCode"`
        //Successful bool `json:"successful"`
        TraceId string `json:"traceId"`
        Vid string `json:"vid"`
        DeviceId string `json:"deviceId"`
    } `json:"data"`
    ErrorMsg string `json:"errorMsg"`
}

type AepResponse struct {
    Data struct {
        DeviceName string `json:"deviceName"`
        DeviceSecret string `json:"deviceSecret"`
        ProductKey string `json:"productKey"`
    } `json:"data"`
    Code int `json:"code"`
    Id string `json:"id"`
}

type SessionByAuthCodeResponse struct {
	Data struct {
		IdentityId           string `json:"identityId"`
		RefreshToken         string `json:"refreshToken"`
		RefreshTokenExpire   int64  `json:"refreshTokenExpire"`
		IotToken             string `json:"iotToken"`
		IotTokenExpire       int64  `json:"iotTokenExpire"`
	} `json:"data"`
}

type Device struct {
    BindTime float64 `json:"bindTime"`
    CategoryKey string `json:"categoryKey"`
    CategoryName string `json:"categoryName"`
    DeviceName string `json:"deviceName"`
    GmtModified float64 `json:"gmtModified"`
    IdentityAlias string `json:"identityAlias"`
    IdentityId string `json:"identityId"`
    IotId string `json:"iotId"`
    IsEdgeGateway bool `json:"isEdgeGateway"`
    NetType string `json:"netType"`
    NickName string `json:"nickName"`
    NodeType string `json:"nodeType"`
    Owned float64 `json:"owned"`
    ProductKey string `json:"productKey"`
    ProductName string `json:"productName"`
    Status float64 `json:"status"`
    ThingType string `json:"thingType"`
}

type ListBindingByAccountResponse struct {
    Code int `json:"code"`
    Data struct {
        Data []Device `json:"data"`
        PageNo float64 `json:"pageNo"`
        PageSize float64 `json:"pageSize"`
        Total float64 `json:"total"`
    } `json:"data"`
    Id string `json:"id"`
}

type RegionResponse struct {
	Data struct {
		MQTTEndpoint string `json:"mqttEndpoint"`
		ApiGatewayEndpoint string `json:"apiGatewayEndpoint"`
		OaApiGatewayEndpoint string `json:"oaApiGatewayEndpoint"`
		RegionId string `json:"regionId"`
	} `json:"data"`
}

type ListingDevByAccountResponse struct {
	// Define the fields based on the response structure
}

type Config struct {
	AppKey    string
	AppSecret string
	Domain    string
}

type Client struct {
	Config Config
}

type CommonParams struct {
	APIVer   string `json:"api_ver"`
	Language string `json:"language"`
	IotToken string `json:"iot_token,omitempty"`
}

type IoTApiRequest struct {
	ID      string                 `json:"id"`
	Params  map[string]interface{} `json:"params"`
	Request CommonParams           `json:"request"`
	Version string                 `json:"version"`
}

func NewCloudIOTGateway() *CloudIOTGateway {
	return &CloudIOTGateway{
		AppKey:    APP_KEY,
		AppSecret: APP_SECRET,
		Domain:    ALIYUN_DOMAIN,
		ClientID:  generateHardwareString(4),
		DeviceSN:  generateHardwareString(12),
		Utdid:     generateHardwareString(24),
	}
}

func generateHardwareString(length int) string {
    hashedUUID := sha256.New()
    hashedUUID.Write([]byte(fmt.Sprintf("%d", time.Now().UnixNano())))
    hash := hashedUUID.Sum(nil)
    
    if length > len(hash) {
        length = len(hash)
    }
    
    return fmt.Sprintf("%x", hash[:length])
}

func (c *Client) DoRequest(endpoint, protocol, method string, headers map[string]string, body IoTApiRequest) (*http.Response, error) {
	url := fmt.Sprintf("%s://%s%s", protocol, c.Config.Domain, endpoint)
	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest(method, url, bytes.NewBuffer(jsonBody))
	if err != nil {
		return nil, err
	}

	req.Header.Set("x-ca-key", c.Config.AppKey)
	req.Header.Set("Content-Type", "application/json")
	for key, value := range headers {
		req.Header.Set(key, value)
	}

    fmt.Println("Request:", req)
	client := &http.Client{Timeout: 10 * time.Second}
	return client.Do(req)
}

func (cg *CloudIOTGateway) Sign(data map[string]string) string {
	keys := []string{"appKey", "clientId", "deviceSn", "timestamp"}
	concatenatedStr := ""
	for _, key := range keys {
		concatenatedStr += fmt.Sprintf("%s%s", key, data[key])
	}

	h := hmac.New(sha1.New, []byte(cg.AppSecret))
	h.Write([]byte(concatenatedStr))
	return fmt.Sprintf("%x", h.Sum(nil))
}

func (cg *CloudIOTGateway) GetRegion(countryCode, authCode string) (*RegionResponse, error) {

    config := new(iot.Config).
		SetAppKey(cg.AppKey).
		SetAppSecret(cg.AppSecret).
		SetDomain(cg.Domain)

    client, err := iot.NewClient(config)
	if err != nil {
		panic(err)
	}

    params := map[string]interface{}{
        "authCode": authCode,
        "type": "THIRD_AUTHCODE",
        "countryCode": countryCode,
	}

	request := new(iot.CommonParams).
		SetApiVer("1.0.2").SetLanguage("en-US")

	body := new(iot.IoTApiRequest).
        SetId(uuid.New().String()).
		SetParams(params).
		SetRequest(request).
        SetVersion("1.0")

    runtime := new(util.RuntimeOptions)
	resp, err := client.DoRequest(tea.String("/living/account/region/get"), tea.String("HTTPS"), tea.String("POST"), nil, body, runtime)

    responseBody, err := ioutil.ReadAll(resp.Body)

    if err != nil {
        return nil, err
    }

    var responseBodyDict map[string]interface{}

    if err := json.Unmarshal(responseBody, &responseBodyDict); err != nil {
        return nil, err
    }

    if code, ok := responseBodyDict["code"].(float64); !ok || int(code) != 200 {
        return nil, fmt.Errorf("error in getting regions: %v", responseBodyDict["msg"])
    }

    var regionResponse RegionResponse
    if err := json.Unmarshal(responseBody, &regionResponse); err != nil {
        return nil, err
    }

    cg.RegionResponse = &regionResponse
    return &regionResponse, nil
}

func (cg *CloudIOTGateway) SessionByAuthCode() error {

    config := new(iot.Config).
		SetAppKey(cg.AppKey).
		SetAppSecret(cg.AppSecret).
		SetDomain(cg.RegionResponse.Data.ApiGatewayEndpoint)

    client, err := iot.NewClient(config)
	if err != nil {
		panic(err)
	}

    params := map[string]interface{}{
			"request": map[string]string{
                "authCode": cg.LoginByOAuthResponse.Data.Data.LoginSuccessResult.Sid,
                "accountType": "OA_SESSION",
                "appKey": cg.AppKey,
			},
		}

	request := new(iot.CommonParams).
		SetApiVer("1.0.4").SetLanguage("en-US")

	body := new(iot.IoTApiRequest).
        SetId(uuid.New().String()).
		SetParams(params).
		SetRequest(request).
        SetVersion("1.0")

    runtime := new(util.RuntimeOptions)
	response, err := client.DoRequest(tea.String("/account/createSessionByAuthCode"), tea.String("HTTPS"), tea.String("POST"), nil, body, runtime)

	responseBody, err := ioutil.ReadAll(response.Body)
	if err != nil {
		return err
	}
	var responseBodyDict map[string]interface{}
	if err := json.Unmarshal(responseBody, &responseBodyDict); err != nil {
		return err
	}

	if code, ok := responseBodyDict["code"].(float64); !ok || int(code) != 200 {
		return fmt.Errorf("error sessionsByAuthCode : %v", responseBodyDict["msg"])
	}

	var sessionResponse SessionByAuthCodeResponse
	if err := json.Unmarshal(responseBody, &sessionResponse); err != nil {
		return err
	}

	cg.SessionByAuthCodeResponse = &sessionResponse
	return nil
}

func (cg *CloudIOTGateway) ListDevices() ([]Device, error) {

    config := new(iot.Config).
		SetAppKey(cg.AppKey).
		SetAppSecret(cg.AppSecret).
		SetDomain(cg.RegionResponse.Data.ApiGatewayEndpoint)

    client, err := iot.NewClient(config)
	if err != nil {
		panic(err)
	}

    params := map[string]interface{}{
            "pageSize": 100,
            "pageNo": 1,
		}

	request := new(iot.CommonParams).
		SetApiVer("1.0.8").
        SetLanguage("en-US").
        SetIotToken(cg.SessionByAuthCodeResponse.Data.IotToken)

	body := new(iot.IoTApiRequest).
        SetId(uuid.New().String()).
		SetParams(params).
		SetRequest(request).
        SetVersion("1.0")

    runtime := new(util.RuntimeOptions)
	response, err := client.DoRequest(tea.String("/uc/listBindingByAccount"), tea.String("HTTPS"), tea.String("POST"), nil, body, runtime)

	responseBody, err := ioutil.ReadAll(response.Body)
	if err != nil {
		return nil, err
	}
	var responseBodyDict map[string]interface{}
	if err := json.Unmarshal(responseBody, &responseBodyDict); err != nil {
		return nil, err
	}

	var listResponse ListBindingByAccountResponse
	if err := json.Unmarshal(responseBody, &listResponse); err != nil {
		return nil, err
	}

	return listResponse.Data.Data, nil
}

func (cg *CloudIOTGateway) AepHandle() error {

    config := new(iot.Config).
		SetAppKey(cg.AppKey).
		SetAppSecret(cg.AppSecret).
		SetDomain(cg.Domain)

    client, err := iot.NewClient(config)
	if err != nil {
		panic(err)
	}

    timeNow := time.Now().Unix()
	dataToSign := map[string]string{
		"appKey":    cg.AppKey,
		"clientId":  cg.ClientID,
		"deviceSn":  cg.DeviceSN,
		"timestamp": fmt.Sprintf("%d", timeNow),
	}
    params := map[string]interface{}{
			"authInfo": map[string]string{
				"clientId":  cg.ClientID,
				"sign":      cg.Sign(dataToSign),
				"deviceSn":  cg.DeviceSN,
				"timestamp": fmt.Sprintf("%d", timeNow),
			},
		}

	request := new(iot.CommonParams).
		SetApiVer("1.0.2").SetLanguage("en-US")

	body := new(iot.IoTApiRequest).
        SetId(uuid.New().String()).
		SetParams(params).
		SetRequest(request).
        SetVersion("1.0")

    runtime := new(util.RuntimeOptions)
	response, err := client.DoRequest(tea.String("/app/aepauth/handle"), tea.String("HTTPS"), tea.String("POST"), nil, body, runtime)

	responseBody, err := ioutil.ReadAll(response.Body)
	if err != nil {
		return err
	}
	var responseBodyDict map[string]interface{}
	if err := json.Unmarshal(responseBody, &responseBodyDict); err != nil {
		return err
	}

	if code, ok := responseBodyDict["code"].(float64); !ok || int(code) != 200 {
		return fmt.Errorf("error in getting mqtt credentials: %v", responseBodyDict["msg"])
	}

	var aepResponse AepResponse
	if err := json.Unmarshal(responseBody, &aepResponse); err != nil {
		return err
	}

	cg.AepResponse = &aepResponse
	return nil
}

func (cg *CloudIOTGateway) Connect() error {
	regionURL := "sdk.openaccount.aliyun.com"
	headers := map[string]string{
		"host":                  regionURL,
		"date":                  time.Now().UTC().Format(http.TimeFormat),
		"X-Ca-Nonce":            fmt.Sprintf("%d", time.Now().UnixNano()),
		"X-Ca-Key":              cg.AppKey,
		"X-Ca-Signaturemethod":  "HmacSHA256",
		"accept":                "application/json",
		"content-type":          "application/x-www-form-urlencoded",
		"user-agent":            "AlibabaCloud (Darwin; arm64) Python/3.12.8 Core/0.3.10 TeaDSL/1",
	}

	bodyParam := map[string]interface{}{
		"config": map[string]interface{}{
			"version":    0,
			"lastModify": 0,
		},
        "context": map[string]interface{}{
			"sdkVersion":  "3.4.2",
			"platformName": "android",
			"netType":      "wifi",
			"appKey":       cg.AppKey,
			"yunOSId":      "",
			"appVersion":   APP_VERSION,
			"utDid":        cg.Utdid,
			"appAuthToken": cg.Utdid,
			"securityToken": cg.Utdid,
		},
        "device": map[string]interface{}{
			"model":           "sdk_gphone_x86_arm",
			"brand":           "goldfish_x86",
			"platformVersion": "30",
		},
	}

	dic := make(map[string]string)
	for k, v := range headers {
		dic[k] = v
	}

	moveHeaders := []string{"x-ca-signature", "x-ca-signature-headers", "accept", "content-md5", "content-type", "date", "host", "token", "user-agent"}
	for _, key := range moveHeaders {
		delete(dic, key)
	}

	keys := make([]string, 0, len(dic))
	for k := range dic {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	signHeaders := strings.Join(keys, ",")
	header := ""
	for _, k := range keys {
		header += fmt.Sprintf("%s:%s\n", k, dic[k])
	}
	header = strings.TrimSpace(header)

	headers["x-ca-signature-headers"] = signHeaders
	stringToSign := fmt.Sprintf("POST\n%s\n\n%s\n%s\n%s\n/api/prd/connect.json?request=%s",
		headers["accept"],
		headers["content-type"],
		headers["date"],
		header,
		jsonToString(bodyParam),
	)

	hash := hmac.New(sha256.New, []byte(cg.AppSecret))
	hash.Write([]byte(stringToSign))
	signature := base64.StdEncoding.EncodeToString(hash.Sum(nil))
	headers["x-ca-signature"] = signature

	req, err := http.NewRequest("POST", fmt.Sprintf("https://%s/api/prd/connect.json?request=%s", regionURL, jsonToString(bodyParam)), nil)
	if err != nil {
		return err
	}

	for k, v := range headers {
		req.Header.Set(k, v)
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
        fmt.Println("resp", resp)
		return err
	}
	defer resp.Body.Close()

	var data map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
        fmt.Println("req", req)
        fmt.Println("resp", resp)
        fmt.Println("data", data)
		return err
	}

	if resp.StatusCode == 200 {
		var connectResp ConnectResponse
        //fmt.Println("data", resp.Body)
        //panic("error")
		if err := mapToStruct(data, &connectResp); err != nil {
			return err
		}
		cg.ConnectResponse = &connectResp
		return nil
	}

	return fmt.Errorf("login exception: %v", data)
}

func (cg *CloudIOTGateway) LoginByOAuth(countryCode, authCode string) (*LoginByOAuthResponse, error) {
	regionURL := cg.RegionResponse.Data.OaApiGatewayEndpoint

	headers := map[string]string{
		"host":                  regionURL,
		"date":                  time.Now().UTC().Format(http.TimeFormat),
		"X-Ca-Nonce":            fmt.Sprintf("%d", time.Now().UnixNano()),
		"X-Ca-Key":              cg.AppKey,
		"X-Ca-Signaturemethod":  "HmacSHA256",
		"accept":                "application/json",
		"content-type":          "application/x-www-form-urlencoded; charset=utf-8",
		"user-agent":            "YourUserAgent",
		"vid":                   cg.ConnectResponse.Data.Vid,
	}

	bodyParam := map[string]interface{}{
		"country":      countryCode,
		"authCode":     authCode,
		"oauthPlateform": 23,
		"oauthAppKey":  cg.AppKey,
		"riskControlInfo": map[string]interface{}{
			"appID":              "com.agilexrobotics",
			"appAuthToken":       "",
			"signType":           "RSA",
			"sdkVersion":         "3.4.2",
			"utdid":              cg.Utdid,
			"umidToken":          cg.Utdid,
			"deviceId":           cg.ConnectResponse.Data.Data.Device.Data.DeviceId,
			"USE_OA_PWD_ENCRYPT": "true",
			"USE_H5_NC":          "true",
		},
	}

	// Get sign header
	dic := make(map[string]string)
	for k, v := range headers {
		dic[k] = v
	}

	moveHeaders := []string{"host", "date", "x-ca-nonce", "x-ca-key", "x-ca-signaturemethod", "accept", "content-type", "user-agent", "vid"}
	for _, key := range moveHeaders {
		delete(dic, key)
	}

	keys := make([]string, 0, len(dic))
	for k := range dic {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	signHeaders := strings.Join(keys, ",")
	header := ""
	for _, k := range keys {
		header += fmt.Sprintf("%s:%s\n", k, dic[k])
	}
	header = strings.TrimSpace(header)

	headers["x-ca-signature-headers"] = signHeaders
	stringToSign := fmt.Sprintf("POST\n%s\n\n%s\n%s\n%s\n/api/prd/loginbyoauth.json?loginByOauthRequest=%s",
		headers["accept"],
		headers["content-type"],
		headers["date"],
		header,
		jsonToString(bodyParam),
	)

	hash := hmac.New(sha256.New, []byte(cg.AppSecret))
	hash.Write([]byte(stringToSign))
	signature := base64.StdEncoding.EncodeToString(hash.Sum(nil))
	headers["x-ca-signature"] = signature

	req, err := http.NewRequest("POST", fmt.Sprintf("https://%s/api/prd/loginbyoauth.json?loginByOauthRequest=%s", regionURL, jsonToString(bodyParam)), nil)
	if err != nil {
		return nil, err
	}

	for k, v := range headers {
		req.Header.Set(k, v)
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}

	defer resp.Body.Close()

	var data map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, err
	}

	if resp.StatusCode == 200 {
		var loginResp LoginByOAuthResponse
		if err := mapToStruct(data, &loginResp); err != nil {
			return nil, err
		}
		cg.LoginByOAuthResponse = &loginResp
		return &loginResp, nil
	}

	return nil, fmt.Errorf("login exception: %v", data)
}

func (cg *CloudIOTGateway) SendCloudCommand(iotID string, command []byte) (string, error) {
	// Check if the IoT token is expired and refresh if necessary
	if cg.SessionByAuthCodeResponse == nil || cg.RegionResponse == nil {
		return "", fmt.Errorf("session or region response is nil")
	}

	// Create the request payload
	messageID := uuid.New().String()
	params := SendCloudCommandParams{
		Args: map[string]string{
			"content": base64.StdEncoding.EncodeToString(command),
		},
		Identifier: "device_protobuf_sync_service",
		IotId:      iotID,
	}

	request := CommonParams{
		APIVer:   "1.0.5",
		Language: "en-US",
		IotToken: cg.SessionByAuthCodeResponse.Data.IotToken,
	}

	body := IoTApiRequest{
		ID:      messageID,
		Params:  map[string]interface{}{"args": params.Args, "identifier": params.Identifier, "iotId": params.IotId},
		Request: request,
		Version: "1.0",
	}

	// Marshal the request body to JSON
	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request body: %w", err)
	}

	// Create the HTTP request
	url := fmt.Sprintf("https://%s/thing/service/invoke", cg.RegionResponse.Data.ApiGatewayEndpoint)
	req, err := http.NewRequest("POST", url, bytes.NewBuffer(bodyBytes))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	// Set the necessary headers
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-ca-key", cg.AppKey)
	req.Header.Set("x-ca-signaturemethod", "HmacSHA256")
	req.Header.Set("accept", "application/json")
	req.Header.Set("date", time.Now().UTC().Format(http.TimeFormat))

	// Sign the request
	signature, err := cg.signRequest(req, bodyBytes)
	if err != nil {
		return "", fmt.Errorf("failed to sign request: %w", err)
	}
	req.Header.Set("x-ca-signature", signature)

	// Send the request
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	// Parse the response
	var response SendCloudCommandResponse
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return "", fmt.Errorf("failed to decode response: %w", err)
	}

	if response.Code != 200 {
		return "", fmt.Errorf("error in sending cloud command: %d - %s", response.Code, response.Message)
	}

	return messageID, nil
}

func (cg *CloudIOTGateway) signRequest(req *http.Request, body []byte) (string, error) {
	// Create the string to sign
	stringToSign := fmt.Sprintf("POST\n%s\n\n%s\n%s\n%s\n%s",
		req.Header.Get("accept"),
		req.Header.Get("content-type"),
		req.Header.Get("date"),
		req.URL.Path,
		string(body),
	)

	// Create the HMAC-SHA256 signature
	h := hmac.New(sha256.New, []byte(cg.AppSecret))
	if _, err := h.Write([]byte(stringToSign)); err != nil {
		return "", fmt.Errorf("failed to write HMAC: %w", err)
	}
	signature := base64.StdEncoding.EncodeToString(h.Sum(nil))

	return signature, nil
}

func jsonToString(v interface{}) string {
	bytes, _ := json.Marshal(v)
	return string(bytes)
}

func mapToStruct(data map[string]interface{}, result interface{}) error {
	bytes, err := json.Marshal(data)
	if err != nil {
		return err
	}
	return json.Unmarshal(bytes, result)
}
