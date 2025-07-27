package cmd

import (
	"fmt"
	"log"
	"os"

	"mammo/aliyuniot"
	"mammo/auth"
	"mammo/mammotion"

	"github.com/spf13/cobra"
)

var username string
var password string

func Login() {

	client, err := auth.ConnectHTTP(username, password)
	if err != nil {
		fmt.Println("Error logging in:", err)
		return
	}

	if client.LoginInfo == nil {
		fmt.Println("Error logging in: LoginInfo is nil")
		return
	}

	fmt.Println("Logged in successfully:", client.LoginInfo.UserInformation.Email)

	country_code := client.LoginInfo.UserInformation.DomainAbbreviation

	cg := aliyuniot.NewCloudIOTGateway()
	_, err = cg.GetRegion(country_code, client.LoginInfo.AuthorizationCode)

	if err != nil {
		fmt.Println("Error getting region:", err)
		return
	}

	err = cg.Connect()
	if err != nil {
		fmt.Println("Error connecting to cloud:", err)
		return
	}
	_, err = cg.LoginByOAuth(country_code, client.LoginInfo.AuthorizationCode)

	if err != nil {
		fmt.Println("IOT login error:", err)
		return
	}
	fmt.Println("IOT login successfull")

	err = cg.AepHandle()
	if err != nil {
		fmt.Println("Error handling AEP:", err)
		return
	}
	fmt.Println("AEP handled successfully")

	err = cg.SessionByAuthCode()
	if err != nil {
		fmt.Println("Error getting session by auth code:", err)
		return
	}
	fmt.Println("Session by auth code successful")

	// Refresh the session to get the definitive, valid iotToken
	err = cg.CheckOrRefreshSession()
	if err != nil {
		fmt.Println("Error refreshing session:", err)
		return
	}

	// Get the list of devices using the refreshed token
	devices, err := cg.ListDevices()
	if err != nil {
		fmt.Println("Error getting devices:", err)
		return
	}

	if len(devices) == 0 {
		fmt.Println("No devices found")
		return
	}

	fmt.Println("Devices found:")
	for _, device := range devices {
		fmt.Println("    Nickname:", device.NickName)
		fmt.Println("    Device ID:", device.DeviceName)
		fmt.Println("    Model:", device.ProductName)
		fmt.Println("    Status:", device.Status)
		fmt.Println("")
	}

	// Assuming we're working with the first device
	firstDevice := devices[0]

	// 1. Create and Configure MQTT Client using the FINAL refreshed iotToken
	mqttClient := mammotion.NewMammotionMQTT(
		cg.RegionResponse.Data.RegionId,
		cg.AepResponse.Data.ProductKey,
		cg.AepResponse.Data.DeviceName,
		cg.AepResponse.Data.DeviceSecret,
		cg.SessionByAuthCodeResponse.Data.IotToken,
		cg,
	)

	// 2. Create MammotionCloud
	mammoCloud := mammotion.NewMammotionCloud(mqttClient, cg)

	// 3. Create MowingDevice
	mowingDevice := mammotion.NewMowingDevice(&firstDevice, *cg, mammoCloud)

	// 4. Create the BaseCloudDevice (for command sending)
	stateManager := mammotion.NewStateManager(mowingDevice)
	cloudDevice := mammotion.NewMammotionBaseCloudDevice(mammoCloud, mowingDevice, stateManager)

	// 5. Connect the cloud client
	mammoCloud.ConnectAsync()

	// 6. Queue command
	fmt.Println("Sending 'move_forward' command...")
	_, err = cloudDevice.QueueCommand("move_forward", map[string]interface{}{"linear": 10})
	if err != nil {
		fmt.Println("Error queuing command:", err)
		return
	}
	fmt.Println("Command queued successfully.")
}

var rootCmd = &cobra.Command{
	Use:   "mammo",
	Short: "A CLI test app for Mammotion devices",
	Long: `This CLI tool is designed for interacting with the Mammotion APIs and MQTT commands.

Examples and usage of this application include:

- Sending MQTT commands to control devices
- Fetching data from Mammotion APIs
- Subscribing to MQTT topics to receive real-time updates
- Managing device configurations and settings`,
	Run: func(cmd *cobra.Command, args []string) {
		if len(args) == 0 {
			log.Fatal("Please provide a command")
		}
		if args[0] == "dock" {
			fmt.Println("wooooof!")
		} else if args[0] == "login" {
			Login()
		} else {
			fmt.Println("Sorry, I don't know that command :(")
		}
	},
}

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() {
	err := rootCmd.Execute()
	if err != nil {
		os.Exit(1)
	}
}

func init() {
	// Here you will define your flags and configuration settings.
	// Cobra supports persistent flags, which, if defined here,
	// will be global for your application.

	//rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is $HOME/.luba.yaml)")

	// Cobra also supports local flags, which will only run
	// when this action is called directly.
	rootCmd.Flags().StringVarP(&username, "username", "u", "", "Username for login")
	rootCmd.Flags().StringVarP(&password, "password", "p", "", "Password for login")
}

