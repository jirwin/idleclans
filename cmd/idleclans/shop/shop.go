package shop

import (
	"errors"
	"fmt"
	"github.com/jirwin/ich/pkg/client"
	"github.com/pterm/pterm"
	"github.com/spf13/cobra"
	"golang.org/x/text/language"
	"golang.org/x/text/message"
)

func Run(cmd *cobra.Command, args []string, c *client.Client) error {
	ctx := cmd.Context()

	if len(args) == 0 {
		cmd.Help()
		return errors.New("no player specified")
	}

	player := args[0]
	if player == "" {
		return errors.New("no player specified")
	}

	shop, err := c.GetPlayerShop(ctx, player)
	if err != nil {
		return err
	}

	if shop == nil {
		pterm.Println(fmt.Sprintf("%s has no items for sale", player))
		return nil
	}

	printer := message.NewPrinter(language.English)

	var tableData [][]string
	for _, item := range shop.ParsedItems {
		if item.Id == 0 {
			continue
		}
		tableData = append(tableData, []string{
			fmt.Sprintf("%d", item.Id),
			"TODO: Lookup Name",
			printer.Sprintf("%d", item.Price),
			printer.Sprintf("%d", item.Amount),
			printer.Sprintf("%d", item.Amount*item.Price),
		})
	}

	pterm.Println(fmt.Sprintf("%s's Shop", player))
	err = pterm.DefaultTable.
		WithHasHeader().
		WithBoxed().
		WithData(append([][]string{{"Item ID", "Name", "Price", "Qty", "Total"}}, tableData...)).Render()
	if err != nil {
		return err
	}

	return nil
}

var Cmd = &cobra.Command{
	Use:     "shop [player]",
	Short:   "Shop Information for a player",
	Aliases: []string{"s"},
}
