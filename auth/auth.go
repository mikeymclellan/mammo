package auth

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
)

const (
	MAMMOTION_AUTH_DOMAIN   = "https://id.mammotion.com"
	MAMMOTION_API_DOMAIN   = "https://domestic.mammotion.com"
	MAMMOTION_CLIENT_ID    = "MADKALUBAS"
	MAMMOTION_CLIENT_SECRET = "GshzGRZJjuMUgd2sYHM7"
)

type Response[T any] struct {
	Data T
	Msg  string
	Code int
}

type UserInformation struct {
    AreaCode string
    AuthType string
    DomainAbbreviation string
    Email string
    UserAccount string
    UserId string
}

type LoginResponseData struct {
	AccessToken string
	AuthorizationCode string
	RefreshToken string
	ExpiresIn float64
    UserInformation *UserInformation
}

type ErrorInfo struct {
	Code string
}

type MammotionHTTP struct {
	headers    map[string]string
	LoginInfo  *LoginResponseData
	response   *Response[map[string]interface{}]
	msg        string
	code       int
}

func NewMammotionHTTP(response *Response[map[string]interface{}]) *MammotionHTTP {
	headers := map[string]string{
		"User-Agent":  "okhttp/3.14.9",
		"App-Version": "google Pixel 2 XL taimen-Android 11,1.11.332",
	}
	if response == nil {
		return &MammotionHTTP{headers: headers}
	}
	var loginInfo *LoginResponseData
	if response.Data != nil {
		loginInfo = LoginResponseDataFromDict(response.Data)
		if loginInfo != nil {
			headers["Authorization"] = fmt.Sprintf("Bearer %s", loginInfo.AccessToken)
		}
	}
	return &MammotionHTTP{
		headers:   headers,
		LoginInfo: loginInfo,
		response:  response,
		msg:       response.Msg,
		code:      response.Code,
	}
}

func UserInformationFromDict(data map[string]interface{}) *UserInformation {
    return &UserInformation{
        AreaCode: data["areaCode"].(string),
        AuthType: data["authType"].(string),
        DomainAbbreviation: data["domainAbbreviation"].(string),
        Email: data["email"].(string),
        UserAccount: data["userAccount"].(string),
        UserId: data["userId"].(string),
    }
}

func LoginResponseDataFromDict(data map[string]interface{}) *LoginResponseData {
	if data == nil {
		return nil
	}

	dataMap, ok := data["data"].(map[string]interface{})
	if !ok {
		return nil
	}

	var userInfo *UserInformation
	if userInfoData, ok := dataMap["userInformation"].(map[string]interface{}); ok {
		userInfo = UserInformationFromDict(userInfoData)
	}

	return &LoginResponseData{
		AccessToken:       dataMap["access_token"].(string),
		AuthorizationCode: dataMap["authorization_code"].(string),
		RefreshToken:      dataMap["refresh_token"].(string),
		ExpiresIn:         dataMap["expires_in"].(float64),
		UserInformation:   userInfo,
	}
}

func ResponseFromDict(data map[string]interface{}) *Response[map[string]interface{}] {
	return &Response[map[string]interface{}]{
		Data: data,
		Msg:  data["msg"].(string),
		Code: int(data["code"].(float64)),
	}
}

func (m *MammotionHTTP) GetAllErrorCodes() (map[string]ErrorInfo, error) {
	client := &http.Client{}
	req, err := http.NewRequest("POST", MAMMOTION_API_DOMAIN+"/user-server/v1/code/record/export-data", nil)
	if err != nil {
		return nil, err
	}
	for key, value := range m.headers {
		req.Header.Set(key, value)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var data map[string]interface{}
	if err := json.Unmarshal(body, &data); err != nil {
		return nil, err
	}
	reader := csv.NewReader(bytes.NewBufferString(data["data"].(string)))
	codes := make(map[string]ErrorInfo)
	for {
		record, err := reader.Read()
		if err != nil {
			break
		}
		var errorInfo ErrorInfo
		if err := json.Unmarshal([]byte(record[0]), &errorInfo); err != nil {
			return nil, err
		}
		codes[errorInfo.Code] = errorInfo
	}
	return codes, nil
}

func (m *MammotionHTTP) OAuthCheck() error {
	client := &http.Client{}
	req, err := http.NewRequest("POST", MAMMOTION_API_DOMAIN+"/user-server/v1/user/oauth/check", nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	var data map[string]interface{}
	if err := json.Unmarshal(body, &data); err != nil {
		return err
	}
	m.response = ResponseFromDict(data)
	return nil
}

func (m *MammotionHTTP) GetStreamSubscription(iotID string) (*Response[map[string]interface{}], error) {
	client := &http.Client{}
	payload := map[string]string{"deviceId": iotID}
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest("POST", MAMMOTION_API_DOMAIN+"/device-server/v1/stream/subscription", bytes.NewBuffer(payloadBytes))
	if err != nil {
		return nil, err
	}
	for key, value := range m.headers {
		req.Header.Set(key, value)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var data map[string]interface{}
	if err := json.Unmarshal(body, &data); err != nil {
		return nil, err
	}
	response := ResponseFromDict(data)
	return response, nil
}

func Login(client *http.Client, username, password string) (*Response[map[string]interface{}], error) {
	req, err := http.NewRequest("POST", MAMMOTION_AUTH_DOMAIN+"/oauth/token", nil)
	if err != nil {
		return nil, err
	}
	q := req.URL.Query()
	q.Add("username", username)
	q.Add("password", password)
	q.Add("client_id", MAMMOTION_CLIENT_ID)
	q.Add("client_secret", MAMMOTION_CLIENT_SECRET)
	q.Add("grant_type", "password")
	req.URL.RawQuery = q.Encode()
	req.Header.Set("User-Agent", "okhttp/3.14.9")
	req.Header.Set("App-Version", "google Pixel 2 XL taimen-Android 11,1.11.332")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var data map[string]interface{}
	if err := json.Unmarshal(body, &data); err != nil {
		return nil, err
	}

	if int(data["code"].(float64)) != 0 {
		return nil, fmt.Errorf("login failed: %s", data["msg"].(string))
	}

	response := ResponseFromDict(data)
	return response, nil
}

func ConnectHTTP(username, password string) (*MammotionHTTP, error) {
	client := &http.Client{}
	loginResponse, err := Login(client, username, password)
	if err != nil {
		return nil, err
	}
	return NewMammotionHTTP(loginResponse), nil
}
