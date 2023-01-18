package main

import (
	"log"
	"os"

	"github.com/github/git-bundle-server/internal/argparse"
)

func all() []argparse.Subcommand {
	return []argparse.Subcommand{
		Delete{},
		Init{},
		Start{},
		Stop{},
		Update{},
		UpdateAll{},
	}
}

func main() {
	cmds := all()

	if len(os.Args) < 2 {
		log.Fatal("usage: git-bundle-server <command> [<options>]\n")
		return
	}

	for i := 0; i < len(cmds); i++ {
		if cmds[i].Name() == os.Args[1] {
			err := cmds[i].Run(os.Args[2:])
			if err != nil {
				log.Fatal("Failed with error: ", err)
			}
		}
	}
}
