package idleclans

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

type itemManager struct {
	sync.RWMutex

	items       map[string]string
	itemsByName map[string]string
	client      *http.Client
	canc        context.CancelFunc
}

func (i *itemManager) convertFromHumanName(name string) string {
	return strings.ReplaceAll(strings.ToLower(name), " ", "_")
}

func (i *itemManager) GetItemID(name string) (string, bool) {
	i.RLock()
	defer i.RUnlock()

	if i.itemsByName == nil {
		return "", false
	}

	id, ok := i.itemsByName[i.convertFromHumanName(name)]
	return id, ok
}

func (i *itemManager) GetItem(id string) (string, bool) {
	i.RLock()
	defer i.RUnlock()

	if i.items == nil {
		return "", false
	}
	name, ok := i.items[id]
	return name, ok
}

func (i *itemManager) fetchItemList(ctx context.Context) error {
	i.Lock()
	defer i.Unlock()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://idleclans.uraxys.dev/api/items/all", nil)
	if err != nil {
		return err
	}

	resp, err := i.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	list := []*itemID{}
	err = json.NewDecoder(resp.Body).Decode(&list)
	if err != nil {
		return err
	}

	for _, item := range list {
		id := strconv.Itoa(item.InternalId)
		i.items[id] = item.NameId
		i.itemsByName[item.NameId] = id
	}

	return nil
}

func (i *itemManager) Run(ctx context.Context) error {
	ctx, i.canc = context.WithCancel(ctx)
	defer i.canc()

	err := i.fetchItemList(ctx)
	if err != nil {
		return err
	}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(24 * time.Hour):
			err = i.fetchItemList(ctx)
			if err != nil {
				return err
			}
		}
	}
}

func (i *itemManager) Close(ctx context.Context) {
	i.canc()
}

type itemID struct {
	NameId     string `json:"name_id"`
	InternalId int    `json:"internal_id"`
}

func newItemManager() *itemManager {
	return &itemManager{
		items:       make(map[string]string),
		itemsByName: make(map[string]string),
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}
