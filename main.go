package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	html "html/template"
	"log"
	"net/http"
	"os"

	"github.com/BurntSushi/toml"
	"github.com/go-playground/validator/v10"
	"github.com/matrix-org/gomatrix"
	"github.com/prometheus/alertmanager/template"
)

var logger *log.Logger
var config Configuration

type Configuration struct {
	Matrix struct {
		Homeserver string `toml:"homeserver" validate:"required,url"`
		RoomID     string `toml:"room_id" validate:"required"`
	} `toml:"matrix"`
	User struct {
		ID    string `toml:"id" validate:"required"`
		Token string `toml:"token" validate:"required"`
	} `toml:"user"`
	HTTP struct {
		Port    int    `toml:"port" validate:"number"`
		Address string `toml:"address" `
		Path    string `toml:"path" `
	} `toml:"http"`
	General struct {
		Debug        bool   `toml:"debug" validate:"boolean"`
		HTMLTemplate string `toml:"html_template" validate:"go-template"`
	} `toml:"general"`
}

func ValidateHTMLTemplate(fl validator.FieldLevel) bool {
	_, err := html.New("html template").Parse(fl.Field().String())
	return err == nil
}

func getDefaultConfig() Configuration {
	c := Configuration{}
	c.HTTP.Port = 9088
	c.HTTP.Address = "localhost"
	c.HTTP.Path = "/alert"
	c.General.Debug = false
	c.General.HTMLTemplate = `
	{{range .Alerts -}}
		[{{ .Status }}] {{ .Labels.instance }} - {{ index .Annotations "summary"}}<br/>
	{{end -}}
	`
	return c
}

func renderHTMLMessage(alerts template.Data) gomatrix.HTMLMessage {
	tpl, _ := html.New("html template").Parse(config.General.HTMLTemplate)
	var buf bytes.Buffer
	tpl.Execute(&buf, alerts)
	return gomatrix.GetHTMLMessage("m.text", buf.String())
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

func handleIncomingHooks(w http.ResponseWriter, r *http.Request, matrixClient *gomatrix.Client, targetRoomID string) {

	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	payload := template.Data{}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	logger.Printf("Received valid hook from %v", r.RemoteAddr)

	msg := renderHTMLMessage(payload)
	logger.Printf("> %v", msg.Body)
	_, err := matrixClient.SendMessageEvent(targetRoomID, "m.room.message", msg)
	if err != nil {
		logger.Printf(">> Could not forward to Matrix: %v", err)
	}

	w.WriteHeader(http.StatusOK)
}

func main() {
	// Initialize logger
	logger = log.New(os.Stdout, "", log.Flags())

	var configPath = flag.String("config", "/etc/matrix-alertmanager-receiver.toml", "Path to configuration file")
	flag.Parse()

	logger.Printf("Reading configuration from %v.", *configPath)
	config = getDefaultConfig()
	_, err := toml.DecodeFile(*configPath, &config)
	if err != nil {
		logger.Fatalf("Could not parse configuration file (%v): %v", *configPath, err)
	}

	// Validate config file
	validate := validator.New(validator.WithRequiredStructEnabled())
	validate.RegisterValidation("go-template", ValidateHTMLTemplate)
	err = validate.Struct(config)
	if err != nil {
		logger.Print("Found errors in the configuration file:")
		for _, err := range err.(validator.ValidationErrors) {
			if err.Tag() == "required" {
				logger.Printf("%s is required", err.Namespace())
			} else {
				logger.Printf("%s failed validation: %s\n", err.Namespace(), err.Tag())
			}
		}
		os.Exit(1)
	}

	// Initialize Matrix client
	matrixClient := getMatrixClient(config.Matrix.Homeserver, config.User.ID, config.User.Token, config.Matrix.RoomID)

	// Initialize HTTP server
	http.HandleFunc(fmt.Sprintf("%s", config.HTTP.Path), func(w http.ResponseWriter, r *http.Request) {
		handleIncomingHooks(w, r, matrixClient, config.Matrix.RoomID)
	})

	var listenAddr = fmt.Sprintf("%v:%v", config.HTTP.Address, config.HTTP.Port)
	logger.Printf("Listening for HTTP requests (webhooks) on %v", listenAddr)
	logger.Fatal(http.ListenAndServe(listenAddr, nil))
}
