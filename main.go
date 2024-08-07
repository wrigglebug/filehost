package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type UploadResponse struct {
	Filename string `json:"filename"`
	URL      string `json:"url"`
}

type ErrorResponse struct {
	Error string `json:"error"`
}

var (
	hostname             string
	port                 string
	uploadDir            string = "./uploaded"
	disallowedExtensions        = map[string]bool{
		".exe":  true,
		".bat":  true,
		".cmd":  true,
		".msi":  true,
		".vbs":  true,
		".scr":  true,
		".html": true,
	}
)

func uploadFile(w http.ResponseWriter, r *http.Request) {
	log.Printf("Received %s request from %s for URL: %s", r.Method, r.RemoteAddr, r.URL.Path)

	err := r.ParseMultipartForm(2 << 30) // 2 GiB limit
	if err != nil {
		log.Printf("Error parsing multipart form: %v", err)
		writeJSONError(w, "Unable to parse form", http.StatusBadRequest)
		return
	}

	files := r.MultipartForm.File["file"]
	if len(files) == 0 {
		writeJSONError(w, "No files uploaded", http.StatusBadRequest)
		return
	}

	err = os.MkdirAll(uploadDir, os.ModePerm)
	if err != nil {
		log.Printf("Error creating upload directory: %v", err)
		writeJSONError(w, "Unable to create directory", http.StatusInternalServerError)
		return
	}

	var responses []UploadResponse

	for _, fileHeader := range files {
		ext := filepath.Ext(fileHeader.Filename)
		if ext == "" {
			writeJSONError(w, "Filename must have an extension", http.StatusBadRequest)
			return
		}

		if disallowedExtensions[ext] {
			writeJSONError(w, "Disallowed file extension", http.StatusBadRequest)
			return
		}

		file, err := fileHeader.Open()
		if err != nil {
			log.Printf("Error opening uploaded file: %v", err)
			writeJSONError(w, "Unable to open uploaded file", http.StatusInternalServerError)
			return
		}
		defer file.Close()

		rand.Seed(time.Now().UnixNano())
		randomString := generateRandomString(6)
		filename := strings.ReplaceAll(fileHeader.Filename, " ", "_")
		newFilename := randomString + "_" + filename

		f, err := os.OpenFile(filepath.Join(uploadDir, newFilename), os.O_WRONLY|os.O_CREATE, 0666)
		if err != nil {
			log.Printf("Error creating file on server: %v", err)
			writeJSONError(w, "Unable to create file on server", http.StatusInternalServerError)
			return
		}
		defer f.Close()

		_, err = io.Copy(f, file)
		if err != nil {
			log.Printf("Error saving file on server: %v", err)
			writeJSONError(w, "Unable to save file on server", http.StatusInternalServerError)
			return
		}

		url := fmt.Sprintf("%s/files/uploaded/%s", hostname, newFilename)
		response := UploadResponse{
			Filename: newFilename,
			URL:      url,
		}
		responses = append(responses, response)
	}

	responseJSON, err := json.Marshal(responses)
	if err != nil {
		log.Printf("Error marshalling JSON: %v", err)
		writeJSONError(w, "Unable to marshal JSON", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write(responseJSON)
}

func writeJSONError(w http.ResponseWriter, message string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	errorResponse := ErrorResponse{Error: message}
	json.NewEncoder(w).Encode(errorResponse)
}

func generateRandomString(length int) string {
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, length)
	for i := range b {
		b[i] = charset[rand.Intn(len(charset))]
	}
	return string(b)
}

func logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/uploaded/") && r.Method == http.MethodGet {
			log.Printf("GET request to /uploaded/: %s", r.URL.Path)
		}
		next.ServeHTTP(w, r)
	})
}

func main() {
	flag.StringVar(&hostname, "hostname", "http://localhost", "The hostname for the URL in the response")
	flag.StringVar(&port, "port", "8080", "The port number for the server")
	flag.Parse()

	http.Handle("/", http.FileServer(http.Dir("./static")))
	http.Handle("/uploaded/", logRequests(http.StripPrefix("/uploaded/", http.FileServer(http.Dir(uploadDir)))))
	http.HandleFunc("/upload", uploadFile)

	serverAddress := fmt.Sprintf(":%s", port)
	fmt.Printf("Server started on %s\n", serverAddress)
	log.Fatal(http.ListenAndServe(serverAddress, nil))
}
