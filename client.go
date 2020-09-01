package nacos

import (
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	jsoniter "github.com/json-iterator/go"
)

const (
	// GET http method GET
	GET = "GET"
	// POST http method POST
	POST = "POST"
	// DELETE http method DELETE
	DELETE = "DELETE"
	// PUT http method PUT
	PUT = "PUT"
)

// jsoniter
var json = jsoniter.ConfigCompatibleWithStandardLibrary

// Config is used to configure the creation of a client
type Config struct {
	Scheme      string
	Hosts       []string
	ContextPath string
	AppName     string
	Namespace   string
	Endpoint    string
	Username    string
	Password    string
	AccessKey   string
	SecretKey   string
	Metadata    map[string]string
	HttpClient  *http.Client
	CacheDir    string
	LogDir      string
	LogLevel    LogLevel
}

// AccessToken
type accessToken struct {
	AccessToken        string `json:"accessToken"`
	TokenTTL           int    `json:"tokenTtl"`
	tokenRefreshWindow int
	lastRefreshTime    time.Time
}

// Client provides a client to the Nacos API
type client struct {
	sync.Mutex
	config       Config
	token        accessToken
	namingClient *namingClient
	configClient *configClient
	logger       Logger
	ctx          context.Context
	cancel       context.CancelFunc
}

func (c *client) Naming() NamingClient {
	c.Lock()
	defer c.Unlock()
	if c.namingClient != nil {
		return c.namingClient
	}
	ns := &namingClient{c: c, serviceInfoMap: make(map[string]*ServiceInfo)}
	ns.pushReceiver = newPushRecevier(ns)
	ns.failover = newFailover(ns)
	ns.heartbeat = newHeartbeat(ns)
	ns.listeners = newServiceChangeListener(ns)
	go func(ns *namingClient) {
		t := time.NewTicker(time.Second * 30)
		for {
			select {
			case <-ns.c.ctx.Done():
				t.Stop()
				return
			case <-t.C:
				ns.refreshSrvIfNeed()
			}
		}
	}(ns)
	go func(ns *namingClient) {
		t := time.NewTicker(time.Second * 5)
		for {
			select {
			case <-ns.c.ctx.Done():
				t.Stop()
				return
			case <-t.C:
				ns.c.tryLogin(ns.getServerList())
			}
		}
	}(ns)
	c.tryLogin(ns.getServerList())
	ns.refreshSrvIfNeed()
	c.namingClient = ns
	return ns
}

func (c *client) Config() ConfigClient {
	return &configClient{c: c}
}

func (c *client) Logger() Logger {
	return c.logger
}

// DefaultConfig returns a new default config
func DefaultConfig() *Config {
	pwd := os.Getenv("HOME")
	cacheDir := path.Join(pwd, "/nacos/cache")
	logDir := path.Join(pwd, "/nacos/log")
	return &Config{
		Namespace:  "public",
		Username:   "nacos",
		Password:   "nacos",
		HttpClient: DefaultClient(),
		CacheDir:   cacheDir,
		LogDir:     logDir,
	}
}

// NewClient returns a new nacos client
func NewClient(config *Config) (Client, error) {
	if len(config.Hosts) == 0 {
		return nil, errors.New("confog.Hosts cannot be empty")
	}

	_config := DefaultConfig()
	if config.Scheme == "" {
		config.Scheme = "http"
	}
	if config.ContextPath == "" {
		config.ContextPath = "/"
	}
	if config.HttpClient == nil {
		config.HttpClient = _config.HttpClient
	}
	if config.Username == "" {
		config.Username = _config.Username
	}
	if config.Password == "" {
		config.Password = _config.Password
	}
	if config.LogDir == "" {
		config.LogDir = _config.LogDir
	}
	fi, err := os.Stat(config.LogDir)
	if err != nil && os.IsNotExist(err) {
		os.MkdirAll(config.LogDir, os.ModePerm)
	} else if err == nil && !fi.IsDir() {
		os.Remove(config.LogDir)
		os.MkdirAll(config.LogDir, os.ModePerm)
	}

	if config.CacheDir == "" {
		config.CacheDir = path.Join(_config.CacheDir, config.Namespace)
	}
	fi, err = os.Stat(config.CacheDir)
	if err != nil && os.IsNotExist(err) {
		os.MkdirAll(config.CacheDir, os.ModePerm)
	} else if err == nil && !fi.IsDir() {
		os.Remove(config.CacheDir)
		os.MkdirAll(config.CacheDir, os.ModePerm)
	}
	if config.LogLevel <= 0 {
		config.LogLevel = LogInfo
	}
	if config.Endpoint != "" {
		config.Hosts = []string{}
	}

	ctx, cancel := context.WithCancel(context.Background())
	client := &client{config: *config, ctx: ctx, cancel: cancel}
	client.logger, _ = NewLogger(config.LogDir+"/nacos.log", config.LogLevel)
	return client, nil
}

func (c *client) tryLogin(servers []string) bool {
	if !c.token.lastRefreshTime.IsZero() || time.Now().Sub(c.token.lastRefreshTime).Milliseconds() < (time.Second*time.Duration(c.token.TokenTTL-c.token.tokenRefreshWindow)).Milliseconds() {
		return true
	}
	for _, server := range servers {
		if c.login(server) {
			c.token.lastRefreshTime = time.Now()
			return true
		}
	}
	return false
}

func (c *client) login(server string) bool {
	r := &Request{
		config: &c.config,
		method: POST,
		path:   "/v1/auth/login",
		params: make(url.Values),
		header: make(http.Header),
	}

	if c.config.Namespace != "" {
		r.params.Set("namespaceId", c.config.Namespace)
	}
	r.params.Set("username", c.config.Username)
	r.params.Set("password", c.config.Password)

	req, _ := r.toHTTPRequest(server)
	resp, err := c.config.HttpClient.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	b, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return false
	}
	var token accessToken
	err = json.Unmarshal(b, &token)
	c.token = token
	if c.token.AccessToken != "" {
		c.token.tokenRefreshWindow = c.token.TokenTTL / 10
	}
	return true
}

type Request struct {
	config *Config
	method string
	path   string
	url    *url.URL
	params url.Values
	body   io.Reader
	header http.Header
	ctx    context.Context
}

func (r *Request) toHTTPRequest(host string) (*http.Request, error) {
	r.url = &url.URL{
		Scheme: r.config.Scheme,
		Host:   host,
		Path:   r.config.ContextPath + r.path,
	}
	r.url.RawQuery = r.params.Encode()
	// Create the HTTP request
	req, err := http.NewRequest(r.method, r.url.String(), r.body)
	if err != nil {
		return nil, err
	}
	req.Header = r.header
	if r.ctx != nil {
		return req.WithContext(r.ctx), nil
	}

	return req, nil
}

func (c *client) DoRequest(r *Request) (resp *http.Response, err error) {
	r.header.Set("Content-Type", "application/x-www-form-urlencoded")
	if c.config.Namespace != "" {
		r.params.Set("namespaceId", c.config.Namespace)
	}
	if c.token.AccessToken != "" {
		r.params.Set("accessToken", c.token.AccessToken)
	}
	if c.config.AppName != "" {
		r.params.Set("app", c.config.AppName)
	} else {
		r.params.Set("app", "unknown")
	}
	if c.config.AccessKey != "" && c.config.SecretKey != "" {
		signData := r.params.Get("serviceName")
		if signData != "" {
			signData = fmt.Sprintf("%d@@%s", time.Now().Unix(), signData)
		} else {
			signData = fmt.Sprintf("%d", time.Now().Unix())
		}
		key := []byte(c.config.SecretKey)
		mac := hmac.New(sha1.New, key)
		mac.Write([]byte(signData))
		sign := base64.StdEncoding.EncodeToString(mac.Sum(nil))
		r.params.Set("signature", sign)
		r.params.Set("data", signData)
		r.params.Set("ak", c.config.AccessKey)
	}
	c.setHeader(r.header)

	if len(c.config.Hosts) == 1 {
		req, err := r.toHTTPRequest(c.config.Hosts[0])
		if err != nil {
			return nil, err
		}
		return c.config.HttpClient.Do(req)
	}
	l := int64(len(c.config.Hosts))
	idx := time.Now().UnixNano() % l
	for i := int64(0); i < l; i++ {
		host := c.config.Hosts[idx]
		req, err := r.toHTTPRequest(host)
		if err != nil {
			idx = (i + 1) % l
			continue
		}
		resp, err = c.config.HttpClient.Do(req)
		if err != nil {
			idx = (i + 1) % l
			continue
		}
		return resp, err
	}

	return nil, errors.New("failed to req API:" + r.path + " after all servers(" + strings.Join(c.config.Hosts, ",") + ") tried: " + err.Error())
}

func (c *client) setHeader(header http.Header) {
	header.Set("Client-Version", ClientVersion)
	header.Set("User-Agent", "Nacos Go client:v"+ClientVersion)
	header.Set("Accept-Encoding", "gzip,deflate,sdch")
	header.Set("Connection", "Keep-Alive")
	header.Set("RequestId", uuid.New().String())
	header.Set("Request-Module", "Naming")
}

type Response struct {
	Code    int    `json:"code"`
	Message string `json:"message,omitempty"`
	Data    string `json:"data"`
}

func (r *Response) Ok() bool {
	return r.Code == 0 || r.Code == 200
}

func (r *Response) BodyTo(obj interface{}) bool {
	if !r.Ok() {
		return false
	}
	err := json.Unmarshal([]byte(r.Data), obj)
	if err != nil {
		return false
	}
	return true
}

func callServer(c *client, r *Request) (response *Response, err error) {
	resp, err := c.DoRequest(r)
	if err != nil {
		return
	}
	response = new(Response)
	c.decode(resp, response)
	if response.Ok() {
		return
	}
	err = errors.New(response.Message)
	return
}

func (c *client) decode(httpResponse *http.Response, resp *Response) error {
	b, err := ioutil.ReadAll(httpResponse.Body)
	if err != nil {
		return err
	}
	defer httpResponse.Body.Close()
	if c.logger.IsDebugEnable() {
		c.logger.Debug("%s", string(b))
	}
	iter := jsoniter.ParseBytes(jsoniter.ConfigDefault, b)
	resp.Code = -1
	for field := iter.ReadObject(); field != ""; field = iter.ReadObject() {
		switch field {
		case "code":
			resp.Code = iter.ReadInt()
		case "data":
			resp.Data = iter.ReadString()
		case "message":
			resp.Message = iter.ReadString()
		}
	}
	if resp.Code == -1 {
		resp.Code = httpResponse.StatusCode
		if resp.Code == 0 || resp.Code == 200 {
			resp.Data = string(b)
		} else {
			resp.Message = string(b)
		}
	}
	return nil
}
