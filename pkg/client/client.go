package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/dgrijalva/jwt-go"
	"io"
	"net/http"
	"path"
	"time"
)

const baseUrl = "realm.mongodb.com/api/client/v2.0"

type Client struct {
	AccessToken  string
	RefreshToken string
	client       *http.Client
}

type Config struct {
	AccessToken  string
	RefreshToken string
}

func (c *Client) refreshAccessToken(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://"+path.Join(baseUrl, "auth/session"), nil)
	if err != nil {
		return err
	}

	req.Header.Add("Authorization", "Bearer "+c.RefreshToken)

	decoded := make(map[string]interface{})

	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	err = json.Unmarshal(respBytes, &decoded)
	if err != nil {
		fmt.Println("error decoding response: ", string(respBytes))
		return err
	}

	if at, ok := decoded["access_token"].(string); ok {
		c.AccessToken = at
	} else {
		return fmt.Errorf("invalid access token: %s", string(respBytes))
	}

	return nil
}

type functionCall struct {
	Name string        `json:"name"`
	Args []interface{} `json:"arguments"`
}

func (c *Client) auth(ctx context.Context) error {
	if c.AccessToken == "" {
		return c.refreshAccessToken(ctx)
	}

	token, _, err := new(jwt.Parser).ParseUnverified(c.AccessToken, jwt.MapClaims{})
	if err != nil {
		return err
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return errors.New("invalid claims")
	}

	var tm time.Time
	switch exp := claims["exp"].(type) {
	case float64:
		tm = time.Unix(int64(exp), 0)
	case json.Number:
		v, _ := exp.Int64()
		tm = time.Unix(v, 0)
	}

	if tm.Sub(time.Now()) < time.Minute {
		return c.refreshAccessToken(ctx)
	}

	return nil
}

func (c *Client) doReq(ctx context.Context, req *http.Request) (*http.Response, error) {
	err := c.auth(ctx)
	if err != nil {
		return nil, err
	}

	req.Header.Add("Authorization", "Bearer "+c.AccessToken)

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}

	return resp, nil
}

func (c *Client) runFunction(ctx context.Context, name string, args []interface{}) ([]byte, error) {
	fc := &functionCall{
		Name: name,
		Args: args,
	}
	var buf bytes.Buffer
	err := json.NewEncoder(&buf).Encode(fc)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://"+path.Join(baseUrl, "app/idleproduction-iddch/functions/call"), &buf)
	if err != nil {
		return nil, err
	}

	resp, err := c.doReq(ctx, req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	return body, nil
}

func NewClient(ctx context.Context, config *Config) (*Client, error) {
	c := &Client{
		AccessToken:  config.AccessToken,
		RefreshToken: config.RefreshToken,
		client:       &http.Client{Timeout: time.Second * 10},
	}

	if c.RefreshToken == "" {
		return nil, errors.New("no refresh token found")
	}

	if c.AccessToken == "" {
		err := c.refreshAccessToken(ctx)
		if err != nil {
			return nil, err
		}
	}

	return c, nil
}
