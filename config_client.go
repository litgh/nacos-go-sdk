package nacos

var _ ConfigClient = new(configClient)

type configClient struct {
	c *client
}

func (cs *configClient) GetConfig(dataID, group string) string {
	return ""
}
func (cs *configClient) GetConfigAndSignListener(dataID, group string, listener EventListener) string {
	return ""
}
func (cs *configClient) AddListener(dataID, group string, listener EventListener) {

}
func (cs *configClient) PublishConfig(dataID, group, content string) {

}
func (cs *configClient) RemoveConfig(dataID, group string) {

}
func (cs *configClient) RemoveListener(dataID, group string) {

}
func (cs *configClient) GetServerStatus() string {
	return ""
}
func (cs *configClient) Shutdown() {

}
