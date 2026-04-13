package cli

import "github.com/spf13/cobra"

func newFoodoraCmd(st *state) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "foodora",
		Short: "foodora (via fd-api)",
	}
	cmd.AddCommand(newCountriesCmd(st))
	cmd.AddCommand(newConfigCmd(st))
	cmd.AddCommand(newCookiesCmd(st))
	cmd.AddCommand(newSessionCmd(st))
	cmd.AddCommand(newLoginCmd(st))
	cmd.AddCommand(newLogoutCmd(st))
	cmd.AddCommand(newOrdersCmd(st))
	cmd.AddCommand(newHistoryCmd(st))
	cmd.AddCommand(newOrderCmd(st))
	cmd.AddCommand(newReorderCmd(st))
	return cmd
}

func newDeliverooCmd(st *state) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "deliveroo",
		Short: "Deliveroo",
	}
	cmd.AddCommand(newDeliverooConfigCmd(st))
	cmd.AddCommand(newDeliverooHistoryCmd(st))
	cmd.AddCommand(newDeliverooOrdersCmd(st))
	return cmd
}

func newUberEatsCmd(st *state) *cobra.Command {
	var trace bool
	cmd := &cobra.Command{
		Use:   "ubereats",
		Short: "Uber Eats",
	}
	cmd.PersistentFlags().BoolVar(&trace, "trace", false, "trace redacted Uber Eats JSON interactions to stderr")
	cmd.AddCommand(newUberEatsConfigCmd(st))
	cmd.AddCommand(newUberEatsLoginCmd(st))
	cmd.AddCommand(newUberEatsLogoutCmd(st))
	cmd.AddCommand(newUberEatsAddressesCmd(st))
	cmd.AddCommand(newUberEatsCartsCmd(st))
	cmd.AddCommand(newUberEatsStoresCmd(st))
	cmd.AddCommand(newUberEatsItemsCmd(st))
	cmd.AddCommand(newUberEatsOrdersCmd(st))
	cmd.AddCommand(newUberEatsHistoryCmd(st))
	cmd.AddCommand(newUberEatsOrderCmd(st))
	attachUberEatsTraceOverrides(cmd, st)
	return cmd
}

func attachUberEatsTraceOverrides(cmd *cobra.Command, st *state) {
	if cmd.Run != nil || cmd.RunE != nil {
		wrapUberEatsTraceOverride(cmd, st)
	}
	for _, child := range cmd.Commands() {
		attachUberEatsTraceOverrides(child, st)
	}
}

func wrapUberEatsTraceOverride(cmd *cobra.Command, st *state) {
	applyTraceOverride := func(c *cobra.Command) {
		if !c.Flags().Changed("trace") {
			return
		}
		trace, err := c.Flags().GetBool("trace")
		if err != nil {
			return
		}
		st.ubereats().Debug = trace
	}
	if oldPreRunE := cmd.PreRunE; oldPreRunE != nil {
		cmd.PreRunE = func(c *cobra.Command, args []string) error {
			applyTraceOverride(c)
			return oldPreRunE(c, args)
		}
		return
	}
	if oldPreRun := cmd.PreRun; oldPreRun != nil {
		cmd.PreRun = func(c *cobra.Command, args []string) {
			applyTraceOverride(c)
			oldPreRun(c, args)
		}
		return
	}
	cmd.PreRun = func(c *cobra.Command, args []string) {
		applyTraceOverride(c)
	}
}
