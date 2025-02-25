package list

import (
	"fmt"
	"net/http"

	"github.com/cli/cli/api"
	"github.com/cli/cli/internal/ghrepo"
	"github.com/cli/cli/pkg/cmdutil"
	"github.com/cli/cli/pkg/iostreams"
	"github.com/cli/cli/utils"
	"github.com/spf13/cobra"
)

const (
	defaultLimit = 10

	Active           WorkflowState = "active"
	DisabledManually WorkflowState = "disabled_manually"
)

type ListOptions struct {
	IO         *iostreams.IOStreams
	HttpClient func() (*http.Client, error)
	BaseRepo   func() (ghrepo.Interface, error)

	PlainOutput bool

	All   bool
	Limit int
}

func NewCmdList(f *cmdutil.Factory, runF func(*ListOptions) error) *cobra.Command {
	opts := &ListOptions{
		IO:         f.IOStreams,
		HttpClient: f.HttpClient,
	}

	cmd := &cobra.Command{
		Use:    "list",
		Short:  "List GitHub Actions workflows",
		Args:   cobra.NoArgs,
		Hidden: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			// support `-R, --repo` override
			opts.BaseRepo = f.BaseRepo

			terminal := opts.IO.IsStdoutTTY() && opts.IO.IsStdinTTY()
			opts.PlainOutput = !terminal

			if opts.Limit < 1 {
				return &cmdutil.FlagError{Err: fmt.Errorf("invalid limit: %v", opts.Limit)}
			}

			if runF != nil {
				return runF(opts)
			}

			return listRun(opts)
		},
	}

	cmd.Flags().IntVarP(&opts.Limit, "limit", "L", defaultLimit, "Maximum number of workflows to fetch")
	cmd.Flags().BoolVarP(&opts.All, "all", "a", false, "Show all workflows, including disabled workflows")

	return cmd
}

func listRun(opts *ListOptions) error {
	repo, err := opts.BaseRepo()
	if err != nil {
		return fmt.Errorf("could not determine base repo: %w", err)
	}

	httpClient, err := opts.HttpClient()
	if err != nil {
		return fmt.Errorf("could not create http client: %w", err)
	}
	client := api.NewClientFromHTTP(httpClient)

	opts.IO.StartProgressIndicator()
	workflows, err := getWorkflows(client, repo, opts.Limit)
	opts.IO.StopProgressIndicator()
	if err != nil {
		return fmt.Errorf("could not get workflows: %w", err)
	}

	if len(workflows) == 0 {
		if !opts.PlainOutput {
			fmt.Fprintln(opts.IO.ErrOut, "No workflows found")
		}
		return nil
	}

	tp := utils.NewTablePrinter(opts.IO)
	cs := opts.IO.ColorScheme()

	for _, workflow := range workflows {
		if workflow.Disabled() && !opts.All {
			continue
		}
		tp.AddField(workflow.Name, nil, cs.Bold)
		tp.AddField(string(workflow.State), nil, nil)
		tp.AddField(fmt.Sprintf("%d", workflow.ID), nil, cs.Cyan)
		tp.EndRow()
	}

	return tp.Render()
}

type WorkflowState string

type Workflow struct {
	Name  string
	ID    int
	State WorkflowState
}

func (w *Workflow) Disabled() bool {
	return w.State != Active
}

type WorkflowsPayload struct {
	Workflows []Workflow
}

func getWorkflows(client *api.Client, repo ghrepo.Interface, limit int) ([]Workflow, error) {
	perPage := limit
	page := 1
	if limit > 100 {
		perPage = 100
	}

	workflows := []Workflow{}

	for len(workflows) < limit {
		var result WorkflowsPayload

		path := fmt.Sprintf("repos/%s/actions/workflows?per_page=%d&page=%d", ghrepo.FullName(repo), perPage, page)

		err := client.REST(repo.RepoHost(), "GET", path, nil, &result)
		if err != nil {
			return nil, err
		}

		for _, workflow := range result.Workflows {
			workflows = append(workflows, workflow)
			if len(workflows) == limit {
				break
			}
		}

		if len(result.Workflows) < perPage {
			break
		}

		page++
	}

	return workflows, nil
}
