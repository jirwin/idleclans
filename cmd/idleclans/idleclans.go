package main

import (
	"errors"
	"github.com/jirwin/ich/cmd/idleclans/price"
	"github.com/jirwin/ich/cmd/idleclans/shop"
	"github.com/jirwin/ich/pkg/client"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var (
	cfgFile string
	root    = &cobra.Command{
		Use:   "idleclans",
		Short: "Idle Clan Information",
	}
)

type clientCmd func(*cobra.Command, []string, *client.Client) error

func cmdWrap(cmd *cobra.Command, fn clientCmd) *cobra.Command {
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		clientConfig := &client.Config{
			AccessToken:  viper.GetString("accesstoken"),
			RefreshToken: viper.GetString("refreshtoken"),
		}

		c, err := client.NewClient(cmd.Context(), clientConfig)
		if err != nil {
			return err
		}

		err = fn(cmd, args, c)
		if err != nil {
			return err
		}

		viper.Set("accesstoken", c.AccessToken)
		viper.Set("refreshtoken", c.RefreshToken)
		err = viper.WriteConfig()
		if err != nil {
			return err
		}

		return nil
	}

	return cmd
}

func init() {
	cobra.OnInitialize(initConfig)

	root.PersistentFlags().StringVarP(&cfgFile, "config", "c", "", "config file")
	root.PersistentFlags().String("access-token", "", "access token")
	root.PersistentFlags().String("refresh-token", "", "refresh token")

	err := viper.BindPFlag("accessToken", root.PersistentFlags().Lookup("access-token"))
	cobra.CheckErr(err)
	err = viper.BindPFlag("refreshToken", root.PersistentFlags().Lookup("refresh-token"))
	cobra.CheckErr(err)

	root.AddCommand(cmdWrap(shop.Cmd, shop.Run))
	root.AddCommand(cmdWrap(price.Cmd, price.Run))
}

func initConfig() {
	if cfgFile != "" {
		viper.SetConfigFile(cfgFile)
	} else {
		viper.SetConfigFile("./config.yaml")
	}

	viper.AutomaticEnv()
	err := viper.ReadInConfig()
	if err != nil && !errors.Is(err, viper.ConfigFileNotFoundError{}) {
		cobra.CheckErr(err)
	}
}

func main() {
	cobra.CheckErr(root.Execute())
}
