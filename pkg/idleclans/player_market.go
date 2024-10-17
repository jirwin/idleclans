package idleclans

import (
	"context"
	"path"
)

type ItemLatestPrice struct {
	ItemId             int `json:"itemId"`
	LowestSellPrice    int `json:"lowestSellPrice"`
	LowestPriceVolume  int `json:"lowestPriceVolume"`
	HighestBuyPrice    int `json:"highestBuyPrice"`
	HighestPriceVolume int `json:"highestPriceVolume"`
}

// GetLatestPrice Gets the latest prices for a specific item, including the lowest price, highest price,
// and optionally the average price.
// https://query.idleclans.com/api/PlayerMarket/items/prices/latest/{itemId}
func (c *Client) GetLatestPrice(ctx context.Context, itemID string) (*ItemLatestPrice, error) {
	if id, ok := c.itemManager.GetItemID(itemID); ok {
		itemID = id
	}

	u, err := c.getBaseURL()
	if err != nil {
		return nil, err
	}

	u.Path = path.Join(u.Path, "PlayerMarket/items/prices/latest", itemID)

	req, err := c.getReq(ctx, "GET", u, nil)
	if err != nil {
		return nil, err
	}

	ret := &ItemLatestPrice{}

	_, err = c.doReq(ctx, req, ret)
	if err != nil {
		return nil, err
	}

	return ret, nil
}
