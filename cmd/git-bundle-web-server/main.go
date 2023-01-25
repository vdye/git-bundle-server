package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"

	"github.com/github/git-bundle-server/internal/argparse"
	"github.com/github/git-bundle-server/internal/core"
)

func parseRoute(path string) (string, string, string, error) {
	elements := strings.FieldsFunc(path, func(char rune) bool { return char == '/' })
	switch len(elements) {
	case 0:
		return "", "", "", fmt.Errorf("empty route")
	case 1:
		return "", "", "", fmt.Errorf("route has owner, but no repo")
	case 2:
		return elements[0], elements[1], "", nil
	case 3:
		return elements[0], elements[1], elements[2], nil
	default:
		return "", "", "", fmt.Errorf("path has depth exceeding three")
	}
}

func serve(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path

	owner, repo, file, err := parseRoute(path)
	if err != nil {
		w.WriteHeader(http.StatusNotFound)
		fmt.Printf("Failed to parse route: %s\n", err)
		return
	}

	route := owner + "/" + repo

	repos, err := core.GetRepositories()
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Printf("Failed to load routes\n")
		return
	}

	repository, contains := repos[route]
	if !contains {
		w.WriteHeader(http.StatusNotFound)
		fmt.Printf("Failed to get route out of repos\n")
		return
	}

	if file == "" {
		file = "bundle-list"
	}

	fileToServe := repository.WebDir + "/" + file
	data, err := os.ReadFile(fileToServe)
	if err != nil {
		w.WriteHeader(http.StatusNotFound)
		fmt.Printf("Failed to read file\n")
		return
	}

	fmt.Printf("Successfully serving content for %s/%s\n", route, file)
	w.Write(data)
}

func startServer(server *http.Server,
	cert string, key string,
	serverWaitGroup *sync.WaitGroup,
) {
	// Add to wait group
	serverWaitGroup.Add(1)

	go func() {
		defer serverWaitGroup.Done()

		// Return error unless it indicates graceful shutdown
		var err error
		if cert != "" {
			err = server.ListenAndServeTLS(cert, key)
		} else {
			err = server.ListenAndServe()
		}

		if err != nil && err != http.ErrServerClosed {
			log.Fatal(err)
		}
	}()

	fmt.Println("Server is running at address " + server.Addr)
}

func main() {
	parser := argparse.NewArgParser("git-bundle-web-server [--port <port>] [--cert <filename> --key <filename>]")
	port := parser.String("port", "8080", "The port on which the server should be hosted")
	cert := parser.String("cert", "", "The path to the X.509 SSL certificate file to use in securely hosting the server")
	key := parser.String("key", "", "The path to the certificate's private key")
	parser.Parse(os.Args[1:])

	// Additional option validation
	p, err := strconv.Atoi(*port)
	if err != nil || p < 0 || p > 65535 {
		parser.Usage("Invalid port '%s'.", *port)
	}
	if (*cert == "") != (*key == "") {
		parser.Usage("Both '--cert' and '--key' are needed to specify SSL configuration.")
	}

	// Configure the server
	mux := http.NewServeMux()
	mux.HandleFunc("/", serve)
	server := &http.Server{
		Handler: mux,
		Addr:    ":" + *port,
	}
	serverWaitGroup := &sync.WaitGroup{}

	// Start the server asynchronously
	startServer(server, *cert, *key, serverWaitGroup)

	// Intercept interrupt signals
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-c
		fmt.Println("Starting graceful server shutdown...")
		server.Shutdown(context.Background())
	}()

	// Wait for server to shut down
	serverWaitGroup.Wait()

	fmt.Println("Shutdown complete")
}
