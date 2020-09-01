package nacos

var _ Client = new(client)

const (
	DefaultGroup   = "DEFAULT_GROUP"
	DefaultCluster = "DEFAULT"
	ClientVersion  = "1.1.3"
)

// Client provides a client to the Nacos API
type Client interface {
	Naming() NamingClient
	Config() ConfigClient
	Logger() Logger
}

// NamingClient provides a client to the Nacos naming API
type NamingClient interface {
	SelectServices(ServiceQueryOptions) (*ServiceList, error)

	SelectService(ServiceQueryOptions) (*Service, error)

	CreateService(ServiceOptions) (*Response, error)

	DeleteService(ServiceOptions) (*Response, error)

	UpdateService(ServiceOptions) (*Response, error)

	RegisterInstance(*Instance) (*Response, error)

	DeRegisterInstance(serviceName, groupName, clusterName, ip string, port int, ephemeral bool) (*Response, error)

	SelectInstance(InstanceQueryOptions) []*Instance

	Subscribe(serviceName, groupName string, clusters []string, listener EventListener)

	Unsubscribe(serviceName, groupName string, clusters []string, listener EventListener)

	Shutdown()
}

// ConfigClient provides a client to the Nacos config API
type ConfigClient interface {
	GetConfig(dataID, group string) string

	GetConfigAndSignListener(dataID, group string, listener EventListener) string

	AddListener(dataID, group string, listener EventListener)

	PublishConfig(dataID, group, content string)

	RemoveConfig(dataID, group string)

	RemoveListener(dataID, group string)

	GetServerStatus() string

	Shutdown()
}
