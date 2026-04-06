// This example shows how to embed the deeplink service in an existing
// application alongside your own routes and a custom processor.
package main

import (
	"log"
	"net/http"
	"os"

	"github.com/yinebebt/deeplink"
)

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	service, err := deeplink.New(deeplink.Config{
		BaseURL:     "http://localhost:" + port,
		Store:       deeplink.NewMemoryStore(),
		TemplateDir: "templates/default",
	})
	if err != nil {
		log.Fatal(err)
	}

	service.Register(deeplink.RedirectProcessor{})
	service.Register(telegramProcessor{})

	// Mount the deeplink handler alongside your own routes.
	mux := http.NewServeMux()
	mux.Handle("/", service.Handler())
	mux.HandleFunc("GET /hello", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("your app routes work alongside deeplink"))
	})

	log.Printf("listening on :%s", port)
	log.Fatal(http.ListenAndServe(":"+port, mux))
}
