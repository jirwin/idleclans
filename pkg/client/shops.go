package client

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
)

type ShopItem struct {
	Id     int `json:"ItemId"`
	Amount int `json:"Amount"`
	Price  int `json:"Price"`
}
type PlayerShop struct {
	Id          string     `json:"_id"`
	PlayerName  string     `json:"PlayerName"`
	ListedItems string     `json:"ListedItems"`
	ParsedItems []ShopItem `json:"-"`
}

func (c *Client) GetPlayerShop(ctx context.Context, playerID string) (*PlayerShop, error) {
	resp, err := c.runFunction(ctx, "GetPlayerShop", []interface{}{playerID})
	if err != nil {
		return nil, err
	}

	if string(resp) == "null" {
		return nil, nil
	}

	shop := &PlayerShop{}
	err = json.Unmarshal(resp, shop)
	if err != nil {
		return shop, nil
	}

	listedItems := strings.ReplaceAll(shop.ListedItems, `\"`, `"`)
	err = json.Unmarshal([]byte(listedItems), &shop.ParsedItems)
	if err != nil {
		return nil, err
	}

	return shop, nil
}

type priceListings struct {
	PlayerName  string `json:"PlayerName"`
	ListedItems []struct {
		ItemId int `json:"ItemId"`
		Amount int `json:"Amount"`
		Price  int `json:"Price"`
	} `json:"ListedItems"`
}

type ItemPrices struct {
	PlayerName string
	Price      int
	Amount     int
	Total      int
}

func (c *Client) GetItemPrices(ctx context.Context, itemID string) ([]*ItemPrices, error) {
	resp, err := c.runFunction(ctx, "GetPlayerSoldItemsByPrice", []interface{}{map[string]string{
		"$numberInt": itemID,
	}})
	if err != nil {
		return nil, err
	}

	out := strings.Trim(strings.ReplaceAll(string(resp), `\"`, `"`), `"`)

	var listings []*priceListings
	err = json.Unmarshal([]byte(out), &listings)
	if err != nil {
		return nil, err
	}

	var ret []*ItemPrices
	for _, listing := range listings {
		for _, item := range listing.ListedItems {
			if itemID != strconv.Itoa(item.ItemId) {
				continue
			}
			ret = append(ret, &ItemPrices{
				PlayerName: listing.PlayerName,
				Price:      item.Price,
				Amount:     item.Amount,
				Total:      item.Price * item.Amount,
			})
		}
	}

	return ret, nil
}
