package price

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
		return errors.New("no item specified")
	}

	itemID := args[0]
	if itemID == "" {
		return errors.New("no item specified")
	}

	prices, err := c.GetItemPrices(ctx, itemID)
	if err != nil {
		return err
	}

	printer := message.NewPrinter(language.English)

	var tableData [][]string

	for _, item := range prices {
		tableData = append(tableData, []string{
			item.PlayerName,
			printer.Sprintf("%d", item.Price),
			printer.Sprintf("%d", item.Amount),
			printer.Sprintf("%d", item.Total),
		})
	}

	pterm.Println(fmt.Sprintf("Prices for item %s", itemID))
	err = pterm.DefaultTable.
		WithHasHeader().
		WithBoxed().
		WithData(append([][]string{{"Player", "Price", "Qty", "Total"}}, tableData...)).Render()
	if err != nil {
		return err
	}

	return nil
}

var Cmd = &cobra.Command{
	Use:     "price [itemID]",
	Short:   "List prices for an item",
	Aliases: []string{"p"},
}
