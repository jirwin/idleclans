package idleclans

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

type Client struct {
	client      *http.Client
	baseURL     string
	itemManager *itemManager
}

func (c *Client) getBaseURL() (*url.URL, error) {
	return url.Parse(c.baseURL)
}

func (c *Client) getReq(ctx context.Context, method string, u *url.URL, body io.Reader) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, u.String(), body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "idleclans-bot/go")
	req.Header.Set("Content-Type", "application/json")

	return req, nil
}

func (c *Client) doReq(ctx context.Context, req *http.Request, body any) (*http.Response, error) {
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("error making request %d", resp.StatusCode)
	}

	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	err = json.Unmarshal(b, &body)
	if err != nil {
		return nil, err
	}

	return resp, nil
}

func (c *Client) Run(ctx context.Context) {
	go c.itemManager.Run(ctx)
}

func (c *Client) Close(ctx context.Context) error {
	c.itemManager.Close(ctx)
	return nil
}

func New() *Client {
	return &Client{
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
		baseURL:     "https://query.idleclans.com/api",
		itemManager: newItemManager(),
	}
}
