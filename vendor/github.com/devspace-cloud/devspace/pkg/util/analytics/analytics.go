package analytics

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/devspace-cloud/devspace/pkg/util/hash"
	"github.com/devspace-cloud/devspace/pkg/util/kubeconfig"
	"github.com/devspace-cloud/devspace/pkg/util/randutil"
	"github.com/devspace-cloud/devspace/pkg/util/yamlutil"
	"github.com/google/uuid"
	homedir "github.com/mitchellh/go-homedir"
	"github.com/pkg/errors"
	"github.com/shirou/gopsutil/process"
)

var token string
var eventEndpoint string
var userEndpoint string
var analyticsConfigFile string
var analyticsInstance *analyticsConfig
var loadAnalyticsOnce sync.Once

const DEFERRED_REQUEST_COMMAND = "send-deferred-request"

// Analytics is an interface for sending data to an analytics service
type Analytics interface {
	Enabled() bool
	Disable() error
	Enable() error
	SendEvent(eventName string, eventProperties map[string]interface{}, userProperties map[string]interface{}) error
	SendCommandEvent(commandArgs []string, commandError error, commandDuration int64) error
	ReportPanics()
	Identify(identifier string) error
	SetVersion(version string)
	SetIdentifyProvider(getIdentity func() string)
	HandleDeferredRequest() error
}

type analyticsConfig struct {
	DistinctID    string `yaml:"distinctId,omitempty"`
	Identifier    string `yaml:"identifier,omitempty"`
	Disabled      bool   `yaml:"disabled,omitempty"`
	LatestUpdate  int64  `yaml:"latestUpdate,omitempty"`
	LatestSession int64  `yaml:"latestSession,omitempty"`

	version          string
	identityProvider *func() string

	kubeLoader kubeconfig.Loader
}

func (a *analyticsConfig) Enabled() bool {
	return !a.Disabled
}

func (a *analyticsConfig) Disable() error {
	if !a.Disabled {
		identValue := map[string]interface{}{
			"device_id": a.DistinctID,

			"user_properties": map[string]interface{}{
				"enabled": false,
			},
		}

		if a.Identifier != "" {
			identValue["user_id"] = a.Identifier
		}

		requestData := map[string]interface{}{
			"parameters": map[string]interface{}{
				"api_key": token,
				"identification": []interface{}{
					identValue,
				},
			},
		}

		err := a.sendRequest(userEndpoint, requestData)
		if err != nil {
			// ignore if request fails
		}

		a.Disabled = true
		return a.save()
	}
	return nil
}

func (a *analyticsConfig) Enable() error {
	if a.Disabled {
		a.Disabled = false
		return a.save()
	}
	return nil
}

func (a *analyticsConfig) Identify(identifier string) error {
	if identifier != a.Identifier {
		if a.Identifier != "" {
			// user was logged in, now different user is logging in => RESET DISTINCT ID
			_ = a.resetDistinctID()
		}
		a.Identifier = identifier

		requestData := map[string]interface{}{
			"parameters": map[string]interface{}{
				"api_key": token,
				"identification": []interface{}{
					map[string]interface{}{
						"device_id": a.DistinctID,
						"user_id":   a.Identifier,
					},
				},
			},
		}

		return a.sendRequest(userEndpoint, requestData)
	}
	return nil
}

func (a *analyticsConfig) SendCommandEvent(commandArgs []string, commandError error, commandDuration int64) error {
	executable, _ := os.Executable()
	command := strings.Join(commandArgs, " ")
	command = strings.Replace(command, executable, "devspace", 1)

	expr := regexp.MustCompile(`^.*\s+(login\s.*--key=?\s*)(.*)(\s.*|$)`)
	command = expr.ReplaceAllString(command, `devspace $1[REDACTED]$3`)

	userProperties := map[string]interface{}{
		"app_version": a.version,
	}
	commandProperties := map[string]interface{}{
		"command":      command,
		"runtime_os":   runtime.GOOS,
		"runtime_arch": runtime.GOARCH,
		"cli_version":  a.version,
	}

	if commandError != nil {
		commandProperties["error"] = strings.Replace(commandError.Error(), "\n", "\\n", -1)
	}

	contextName, err := a.kubeLoader.GetCurrentContext()
	if contextName != "" && err == nil {
		spaceID, cloudProvider, _ := a.kubeLoader.GetSpaceID(contextName)

		if spaceID != 0 {
			commandProperties["space_id"] = spaceID
			commandProperties["cloud_provider"] = cloudProvider
			userProperties["has_spaces"] = true
		}

		kubeConfig, err := a.kubeLoader.LoadRawConfig()
		if err == nil {
			if context, ok := kubeConfig.Contexts[contextName]; ok {
				if cluster, ok := kubeConfig.Clusters[context.Cluster]; ok {
					commandProperties["kube_server"] = cluster.Server
				}

				commandProperties["kube_namespace"] = hash.String(context.Namespace)
			}
		}
		commandProperties["kube_context"] = hash.String(contextName)
	}

	if commandDuration != 0 {
		commandProperties["duration"] = strconv.FormatInt(commandDuration, 10) + "ms"
	}

	if regexp.MustCompile(`^.*\s+(use\s+space\s.*--get-token((\s*)|$))`).MatchString(command) {
		return a.SendEvent("kube-context", commandProperties, userProperties)
	}
	return a.SendEvent("command", commandProperties, userProperties)
}

func (a *analyticsConfig) SendEvent(eventName string, eventProperties map[string]interface{}, userProperties map[string]interface{}) error {
	if !a.Disabled && token != "" {
		now := time.Now()

		insertID, err := randutil.GenerateRandomString(9)
		if err != nil {
			return errors.Errorf("Couldn't generate random insert_id for analytics: %v", err)
		}
		eventData := map[string]interface{}{}
		eventData["insert_id"] = insertID + strconv.FormatInt(now.Unix(), 16)
		eventData["event_type"] = eventName
		eventData["ip"] = "$remote"

		if _, ok := eventData["app_version"]; !ok {
			eventData["app_version"] = a.version
		}

		if _, ok := eventData["session_id"]; !ok {
			sessionID, err := a.getSessionID()
			if err != nil {
				return err
			}
			eventData["session_id"] = sessionID
		}

		if a.identityProvider != nil {
			getIdentity := *a.identityProvider
			a.Identify(getIdentity())
		}

		userProperties["enabled"] = true

		if a.Identifier != "" {
			eventData["user_id"] = a.Identifier
			eventData["user_properties"] = userProperties
		} else {
			eventData["device_id"] = a.DistinctID
		}

		// save session and identity
		err = a.save()
		if err != nil {
			// ignore if save fails
		}

		eventData["event_properties"] = eventProperties

		requestBody := map[string]interface{}{}
		requestBody["api_key"] = token
		requestBody["events"] = []interface{}{
			eventData,
		}
		requestData := map[string]interface{}{
			"body": requestBody,
		}

		return a.sendRequest(eventEndpoint, requestData)
	}
	return nil
}

func (a *analyticsConfig) getSessionID() (int64, error) {
	now := time.Now()
	sessionExpired := time.Unix(a.LatestUpdate*int64(time.Millisecond), 0).Add(time.Minute * 30).Before(now)
	a.LatestUpdate = now.UnixNano() / int64(time.Millisecond)

	if a.LatestSession == 0 || sessionExpired {
		a.LatestSession = a.LatestUpdate - (a.LatestUpdate % (24 * 60 * 60 * 1000))
	}
	return a.LatestSession, nil
}

func (a *analyticsConfig) ReportPanics() {
	if r := recover(); r != nil {
		err := errors.Errorf("Panic: %v\n%v", r, string(debug.Stack()))

		a.SendCommandEvent(os.Args, err, GetProcessDuration())
	}
}

func (a *analyticsConfig) SetVersion(version string) {
	if version == "" {
		version = "dev"
	}
	a.version = version
}

func (a *analyticsConfig) SetIdentifyProvider(getIdentity func() string) {
	a.identityProvider = &getIdentity
}

func (a *analyticsConfig) sendRequest(requestURL string, data map[string]interface{}) error {
	if !a.Disabled && token != "" {
		var err error
		jsonRequestBody := []byte{}
		requestURL, err := url.Parse(requestURL)

		if requestBody, ok := data["body"]; ok {
			jsonRequestBody, err = json.Marshal(requestBody)
			if err != nil {
				return errors.Errorf("Couldn't marshal analytics data to json: %v", err)
			}
		}

		if requestParams, ok := data["parameters"]; ok {
			params := url.Values{}
			paramsMap := requestParams.(map[string]interface{})
			for key := range paramsMap {
				paramValueMap, isMap := paramsMap[key].(map[string]interface{})
				paramValueArray, isArray := paramsMap[key].([]interface{})
				if isMap || isArray {
					var paramValue interface{}
					if isMap {
						paramValue = paramValueMap
					}
					if isArray {
						paramValue = paramValueArray
					}
					jsonParam, err := json.Marshal(paramValue)
					if err != nil {
						return errors.Errorf("Couldn't marshal analytics data to json: %v", err)
					}
					params.Add(key, string(jsonParam))
				} else {
					params.Add(key, paramsMap[key].(string))
				}
			}
			requestURL.RawQuery = params.Encode()
		}

		headers := map[string][]string{
			"Content-Type": []string{"application/json"},
			"Accept":       []string{"*/*"},
		}
		jsonHeaders, err := json.Marshal(headers)
		if err != nil {
			return errors.Errorf("Couldn't marshal analytics headers: %v", err)
		}

		args := []string{DEFERRED_REQUEST_COMMAND, "POST", base64.StdEncoding.EncodeToString([]byte(requestURL.String())), base64.StdEncoding.EncodeToString(jsonHeaders), base64.StdEncoding.EncodeToString(jsonRequestBody)}
		executable, err := os.Executable()
		if err != nil {
			executable = os.Args[0]
		}

		cmd := exec.Command(executable, args...)

		return cmd.Start()
	}
	return nil
}

// HandleDeferredRequest sends a request if args are: executable, DEFERRED_REQUEST_COMMAND
func (a *analyticsConfig) HandleDeferredRequest() error {
	if len(os.Args) < 5 || os.Args[1] != DEFERRED_REQUEST_COMMAND {
		return nil
	}

	httpMethod := os.Args[2]
	requestURL, err := base64.StdEncoding.DecodeString(os.Args[3])
	if err != nil {
		return errors.Errorf("Couldn't base64.decode request URL: %v", err)
	}

	jsonRequestHeaders, err := base64.StdEncoding.DecodeString(os.Args[4])
	if err != nil {
		return errors.Errorf("Couldn't base64.decode request headers: %v", err)
	}

	requestBody := []byte{}
	if len(os.Args) > 5 {
		requestBody, err = base64.StdEncoding.DecodeString(os.Args[5])
		if err != nil {
			return errors.Errorf("Couldn't base64.decode request body: %v", err)
		}
	}

	requestHeaders := map[string][]string{}
	err = json.Unmarshal([]byte(jsonRequestHeaders), &requestHeaders)
	if err != nil {
		return errors.Errorf("Couldn't unmarshal request headers: %v", err)
	}

	request, err := http.NewRequest(httpMethod, string(requestURL), bytes.NewBuffer(requestBody))
	if err != nil {
		return errors.Errorf("Error creating request to analytics endpoint: %v", err)
	}
	request.Header = requestHeaders
	client := &http.Client{}

	response, err := client.Do(request)
	if err != nil {
		return errors.Errorf("Error sending request to analytics endpoint: %v", err)
	}
	defer response.Body.Close()
	body, _ := ioutil.ReadAll(response.Body)

	if response.StatusCode != 200 {
		return fmt.Errorf("Analytics returned HTTP code %d: %s", response.StatusCode, body)
	}

	os.Exit(0)

	return nil
}

func (a *analyticsConfig) resetDistinctID() error {
	DistinctID, err := uuid.NewRandom()
	if err != nil {
		return errors.Errorf("Couldn't create UUID: %v", err)
	}
	a.DistinctID = DistinctID.String()

	return nil
}

func (a *analyticsConfig) save() error {
	analyticsConfigFilePath, err := a.getAnalyticsConfigFilePath()
	if err != nil {
		return errors.Errorf("Couldn't determine config file: %v", err)
	}

	err = yamlutil.WriteYamlToFile(a, analyticsConfigFilePath)
	if err != nil {
		return errors.Errorf("Couldn't save analytics config file %s: %v", analyticsConfigFilePath, err)
	}
	return nil
}

func (a *analyticsConfig) getAnalyticsConfigFilePath() (string, error) {
	homedir, err := homedir.Dir()
	if err != nil {
		return "", err
	}

	return filepath.Join(homedir, analyticsConfigFile), nil
}

// GetAnalytics retrieves the analytics client
func GetAnalytics() (Analytics, error) {
	var err error

	loadAnalyticsOnce.Do(func() {
		analyticsInstance = &analyticsConfig{
			kubeLoader: kubeconfig.NewLoader(),
		}

		analyticsConfigFilePath, err := analyticsInstance.getAnalyticsConfigFilePath()
		if err != nil {
			err = errors.Errorf("Couldn't determine config file: %v", err)
			return
		}
		_, err = os.Stat(analyticsConfigFilePath)
		if err == nil {
			err := yamlutil.ReadYamlFromFile(analyticsConfigFilePath, analyticsInstance)
			if err != nil {
				err = errors.Errorf("Couldn't read analytics config file %s: %v", analyticsConfigFilePath, err)
				return
			}
		}

		if analyticsInstance.DistinctID == "" {
			err = analyticsInstance.resetDistinctID()
			if err != nil {
				err = errors.Errorf("Couldn't reset analytics distinct id: %v", err)
				return
			}

			err = analyticsInstance.save()
			if err != nil {
				err = errors.Errorf("Couldn't save analytics config: %v", err)
				return
			}
		}

		c := make(chan os.Signal, 1)
		signal.Notify(c, os.Interrupt)

		go func() {
			defer func() {
				if r := recover(); r != nil {
					// Fail silently
				}
			}()

			<-c

			analyticsInstance.SendCommandEvent(os.Args, errors.New("interrupted"), GetProcessDuration())
			signal.Stop(c)

			pid := os.Getpid()
			sigterm(pid)
		}()
	})
	return analyticsInstance, err
}

// SetConfigPath sets the config patch
func SetConfigPath(path string) {
	analyticsConfigFile = path
}

// GetProcessDuration returns the process duration
func GetProcessDuration() int64 {
	pid := os.Getpid()
	p, err := process.NewProcess(int32(pid))
	if err == nil {
		procCreateTime, err := p.CreateTime()
		if err == nil {
			return time.Now().UnixNano()/int64(time.Millisecond) - procCreateTime
		}
	}

	return 0
}
