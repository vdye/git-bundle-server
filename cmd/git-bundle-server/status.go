package main

import (
	"context"
	"fmt"

	"github.com/github/git-bundle-server/cmd/utils"
	"github.com/github/git-bundle-server/internal/argparse"
	"github.com/github/git-bundle-server/internal/core"
	"github.com/github/git-bundle-server/internal/log"
)

type statusCmd struct {
	logger    log.TraceLogger
	container *utils.DependencyContainer
}

func NewStatusCommand(logger log.TraceLogger, container *utils.DependencyContainer) argparse.Subcommand {
	return &statusCmd{
		logger:    logger,
		container: container,
	}
}

func (statusCmd) Name() string {
	return "status"
}

func (statusCmd) Description() string {
	return `
Print status information about the bundle server and its configured routes.`
}

func (s *statusCmd) printServerInfo(ctx context.Context) error {
	fmt.Println("Server")
	fmt.Println("------")
	fmt.Printf("Web server daemon:	%s\n", "Stopped")
	fmt.Printf("Cron schedule:		%s\n", "Running")
	fmt.Print("\n")

	return nil
}

func (s *statusCmd) Run(ctx context.Context, args []string) error {
	parser := argparse.NewArgParser(s.logger, "git-bundle-server status [<route>]")
	parser.Parse(ctx, args)

	// To separate the output from the command
	fmt.Print("\n")

	err := s.printServerInfo(ctx)
	if err != nil {
		return s.logger.Error(ctx, err)
	}

	repoProvider := utils.GetDependency[core.RepositoryProvider](ctx, s.container)

	repos, err := repoProvider.GetRepositories(ctx)
	if err != nil {
		return s.logger.Error(ctx, err)
	}

	if len(repos) == 0 {
		fmt.Println("No repositories are configured")
	} else {
		fmt.Println("Configured routes")
		fmt.Println("-----------------")
		for _, repo := range repos {
			fmt.Printf("* %s\n", repo.Route)
		}
		fmt.Print("\n")
	}

	return nil
}
