package tableview

import (
	"fmt"
	"strings"
	"text/tabwriter"

	"github.com/phenixrizen/rift/internal/state"
)

func RenderClusters(rows []state.ClusterRecord) string {
	var b strings.Builder
	w := tabwriter.NewWriter(&b, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "Env\tAccount\tRole\tRegion\tCluster\tAWS Profile\tKube Context")
	for _, row := range rows {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			row.Env,
			accountLabel(row.AccountName, row.AccountID),
			row.RoleName,
			row.Region,
			row.ClusterName,
			row.AWSProfile,
			row.KubeContext,
		)
	}
	_ = w.Flush()
	return b.String()
}

func accountLabel(name, id string) string {
	if strings.TrimSpace(name) == "" {
		return id
	}
	if strings.TrimSpace(id) == "" {
		return name
	}
	return fmt.Sprintf("%s (%s)", name, id)
}
