package cmd

import (
	"custom-vm-autoscaler/internal/cmd/run"
	"strings"

	"github.com/spf13/cobra"
)

const (
	descriptionShort = `Autoscaler for virtual machines with services control`
	descriptionLong  = `
	Autoscaler for virtual machines with services control. This tool allows you to
	manage the number of virtual machines in a managed instance group based on
	the status of a service. Before removing a node, the tool checks if the service
	is still running.
	`
)

func NewRootCommand(name string) *cobra.Command {
	c := &cobra.Command{
		Use:   name,
		Short: descriptionShort,
		Long:  strings.ReplaceAll(descriptionLong, "\t", ""),
	}

	c.AddCommand(
		run.NewCommand(),
	)

	return c
}
