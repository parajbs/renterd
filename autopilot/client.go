package autopilot

import (
	"go.sia.tech/jape"
	"go.sia.tech/renterd/api"
)

// A Client provides methods for interacting with a renterd API server.
type Client struct {
	c jape.Client
}

// NewClient returns a client that communicates with a renterd store server
// listening on the specified address.
func NewClient(addr, password string) *Client {
	return &Client{jape.Client{
		BaseURL:  addr,
		Password: password,
	}}
}

func (c *Client) Actions() (actions []api.Action, err error) {
	err = c.c.GET("/actions", &actions)
	return
}

func (c *Client) SetConfig(cfg api.AutopilotConfig) error {
	return c.c.PUT("/config", cfg)
}

func (c *Client) Config() (cfg api.AutopilotConfig, err error) {
	err = c.c.GET("/config", &cfg)
	return
}

func (c *Client) Status() (uint64, error) {
	var resp api.AutopilotStatusResponseGET
	err := c.c.GET("/status", &resp)
	return resp.CurrentPeriod, err
}

func (c *Client) Trigger() (res string, err error) {
	err = c.c.POST("/debug/trigger", nil, &res)
	return
}
