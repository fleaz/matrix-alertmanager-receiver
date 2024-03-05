package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/BurntSushi/toml"
	"github.com/matrix-org/gomatrix"
	"github.com/prometheus/alertmanager/template"
)

var logger *log.Logger
var config Configuration

type Configuration struct {
	Matrix struct {
		Homeserver string `toml:"homeserver"`
		RoomID     string `toml:"room_id"`
	} `toml:"matrix"`
	User struct {
		ID    string `toml:"id"`
		Token string `toml:"token"`
	} `toml:"user"`
	HTTP struct {
		Port    int    `toml:"port"`
		Address string `toml:"address"`
	} `toml:"http"`
}

func getMatrixMessageFor(alert template.Alert) gomatrix.HTMLMessage {
	var prefix string
	switch alert.Status {
	case "firing":
		prefix = "<strong><font color=\"#ff0000\">FIRING</font></strong> "
	case "resolved":
		prefix = "<strong><font color=\"#33cc33\">RESOLVED</font></strong> "
	default:
		prefix = fmt.Sprintf("<strong>%v</strong> ", alert.Status)
	}

	return gomatrix.GetHTMLMessage("m.text", prefix+alert.Labels["name"]+" >> "+alert.Annotations["summary"])
}

func getMatrixClient(homeserver string, user string, token string, targetRoomID string) *gomatrix.Client {
	logger.Printf("Connecting to Matrix Homserver %v as %v.", homeserver, user)
	matrixClient, err := gomatrix.NewClient(homeserver, user, token)
	if err != nil {
		logger.Fatalf("Could not log in to Matrix Homeserver (%v): %v", homeserver, err)
	}

	joinedRooms, err := matrixClient.JoinedRooms()
	if err != nil {
		logger.Fatalf("Could not fetch Matrix rooms: %v", err)
	}

	for _, roomID := range joinedRooms.JoinedRooms {
		if targetRoomID == roomID {
			logger.Printf("%v is already part of %v.", user, targetRoomID)
			return matrixClient
		}
	}

	logger.Printf("Joining %v.", targetRoomID)
	_, err = matrixClient.JoinRoom(targetRoomID, "", nil)
	if err != nil {
		logger.Fatalf("Failed to join %v: %v", targetRoomID, err)
	}

	return matrixClient
}

func handleIncomingHooks(w http.ResponseWriter, r *http.Request,
	matrixClient *gomatrix.Client, targetRoomID string) {

	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	payload := template.Data{}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		w.WriteHeader(http.StatusBadRequest)
	}

	logger.Printf("Received valid hook from %v", r.RemoteAddr)

	for _, alert := range payload.Alerts {
		msg := getMatrixMessageFor(alert)
		logger.Printf("> %v", msg.Body)
		_, err := matrixClient.SendMessageEvent(targetRoomID, "m.room.message", msg)
		if err != nil {
			logger.Printf(">> Could not forward to Matrix: %v", err)
		}
	}

	w.WriteHeader(http.StatusOK)
}

func main() {
	// Initialize logger.
	logger = log.New(os.Stdout, "", log.Flags())

	// We use a configuration file since we need to specify secrets, and read
	// everything else from it to keep things simple.
	var configPath = flag.String("config", "/etc/matrix-alertmanager-receiver.toml", "Path to configuration file")
	flag.Parse()

	logger.Printf("Reading configuration from %v.", *configPath)
	_, err := toml.DecodeFile(*configPath, &config)
	if err != nil {
		logger.Fatalf("Could not parse configuration file (%v): %v", *configPath, err)
	}

	// TODO: Fix validation
	// for _, field := range []string{"matrix.homeserver", "MXID", "MXToken", "TargetRoomID", "HTTPPort"} {
	// 	if !md.IsDefined(field) {
	// 		logger.Fatalf("Field %v is not set in config. Exiting.", field)
	// 	}
	// }

	// Initialize Matrix client.
	matrixClient := getMatrixClient(
		config.Matrix.Homeserver, config.User.ID, config.User.Token, config.Matrix.RoomID)

	// Initialize HTTP server.
	http.HandleFunc("/alert", func(w http.ResponseWriter, r *http.Request) {
		handleIncomingHooks(w, r, matrixClient, config.Matrix.RoomID)
	})

	var listenAddr = fmt.Sprintf("%v:%v", config.HTTP.Address, config.HTTP.Port)
	logger.Printf("Listening for HTTP requests (webhooks) on %v", listenAddr)
	logger.Fatal(http.ListenAndServe(listenAddr, nil))
}
