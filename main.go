package main

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/auth0/go-auth0"
	"github.com/auth0/go-auth0/management"
	"github.com/joho/godotenv"
	"github.com/spf13/cobra"
)

func getAuth0Client(ctx context.Context, domainEnv, clientIDEnv, clientSecretEnv string) (*management.Management, error) {
	domain := os.Getenv(domainEnv)
	clientID := os.Getenv(clientIDEnv)
	clientSecret := os.Getenv(clientSecretEnv)

	if domain == "" || clientID == "" || clientSecret == "" {
		return nil, fmt.Errorf("%s credentials are missing. Please check your .env file", domainEnv)
	}

	return management.New(domain, management.WithClientCredentials(ctx, clientID, clientSecret))
}

func exportUsers(ctx context.Context, m *management.Management) (string, error) {
	exportFields := []map[string]interface{}{
		{"name": "user_id"},
		{"name": "email"},
		{"name": "name"},
		{"name": "user_metadata"},
		{"name": "app_metadata"},
		{"name": "created_at"},
		{"name": "updated_at"},
		{"name": "email_verified"},
	}

	exportJob := &management.Job{
		ConnectionID: auth0.String(os.Getenv("SOURCE_CONNECTION_ID")),
		Format:       auth0.String("json"),
		Limit:        auth0.Int(50000),
		Fields:       exportFields,
	}

	err := m.Job.ExportUsers(ctx, exportJob)
	if err != nil {
		return "", err
	}

	return *exportJob.ID, nil
}

func checkJobStatus(ctx context.Context, m *management.Management, jobID string) (string, error) {
	job, err := m.Job.Read(ctx, jobID)
	if err != nil {
		return "", err
	}

	if *job.Status == "completed" {
		return *job.Location, nil
	}

	return "", fmt.Errorf("job not completed yet")
}

func downloadFile(url string, filename string) error {
	resp, err := http.Get(url)
	if err != nil {
		return fmt.Errorf("failed to download file: %w", err)
	}
	defer resp.Body.Close()

	out, err := os.Create(filename)
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}
	defer out.Close()

	_, err = io.Copy(out, resp.Body)
	if err != nil {
		return fmt.Errorf("failed to write to file: %w", err)
	}

	fmt.Printf("File downloaded successfully as: %s\n", filename)
	return nil
}

func unzipGZFile(gzFile string) ([]byte, error) {
	file, err := os.Open(gzFile)
	if err != nil {
		return nil, fmt.Errorf("failed to open gz file: %w", err)
	}
	defer file.Close()

	gzReader, err := gzip.NewReader(file)
	if err != nil {
		return nil, fmt.Errorf("failed to create gzip reader: %w", err)
	}
	defer gzReader.Close()

	var result bytes.Buffer
	_, err = io.Copy(&result, gzReader)
	if err != nil {
		return nil, fmt.Errorf("failed to read gz file: %w", err)
	}

	return result.Bytes(), nil
}

func splitJSONData(reader io.Reader, maxChunkSize int, emailVerify bool) ([][]map[string]interface{}, error) {
	var chunks [][]map[string]interface{}
	chunk := []map[string]interface{}{}
	chunkSize := 0

	decoder := json.NewDecoder(reader)

	for {
		var user map[string]interface{}
		if err := decoder.Decode(&user); err != nil {
			if err == io.EOF {
				break
			}
			return nil, fmt.Errorf("failed to decode JSON: %w", err)
		}

		user["email_verified"] = emailVerify

		userData, err := json.Marshal(user)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal user: %w", err)
		}

		userSize := len(userData)
		if chunkSize+userSize > maxChunkSize {
			chunks = append(chunks, chunk)
			chunk = []map[string]interface{}{}
			chunkSize = 0
		}

		chunk = append(chunk, user)
		chunkSize += userSize
	}

	if len(chunk) > 0 {
		chunks = append(chunks, chunk)
	}

	return chunks, nil
}

func checkImportJobStatus(ctx context.Context, m *management.Management, jobID string) error {
	interval := 5 * time.Second
	maxInterval := 30 * time.Second
	totalWait := 0 * time.Second
	maxWait := 5 * time.Minute

	for totalWait < maxWait {
		job, err := m.Job.Read(ctx, jobID)
		if err != nil {
			return fmt.Errorf("failed to read job status: %w", err)
		}

		switch *job.Status {
		case "completed":
			fmt.Printf("Import job %s completed successfully.\n", jobID)
			return nil
		case "failed":
			return fmt.Errorf("import job %s failed", jobID)
		}

		fmt.Printf("Import job %s in progress. Waiting %v...\n", jobID, interval)
		time.Sleep(interval)
		totalWait += interval

		if interval < maxInterval {
			interval *= 2
		}
	}

	return fmt.Errorf("import job %s timed out", jobID)
}

func importUsersChunk(ctx context.Context, m *management.Management, users []map[string]interface{}) error {
	importJob := &management.Job{
		ConnectionID: auth0.String(os.Getenv("DESTINATION_CONNECTION_ID")),
		Users:        users,
		Upsert:       auth0.Bool(true),
	}

	err := m.Job.ImportUsers(ctx, importJob)
	if err != nil {
		return fmt.Errorf("failed to import users: %w", err)
	}

	return checkImportJobStatus(ctx, m, *importJob.ID)
}

func main() {
	err := godotenv.Load()
	if err != nil {
		log.Println("Warning: .env file could not be loaded. Make sure environment variables are set properly.")
	}

	ctx := context.Background()

	sourceClient, err := getAuth0Client(ctx, "SOURCE_DOMAIN", "SOURCE_CLIENT_ID", "SOURCE_CLIENT_SECRET")
	if err != nil {
		log.Printf("Error: Failed to create Auth0 source client: %v\n", err)
		return
	}

	targetClient, err := getAuth0Client(ctx, "DESTINATION_DOMAIN", "DESTINATION_CLIENT_ID", "DESTINATION_CLIENT_SECRET")
	if err != nil {
		log.Printf("Error: Failed to create Auth0 target client: %v\n", err)
		return
	}

	var rootCmd = &cobra.Command{Use: "auth0-cli"}

	var exportCmd = &cobra.Command{
		Use:   "export",
		Short: "Export users from the source Auth0 tenant",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Println("Starting user export from source tenant...")

			jobID, err := exportUsers(ctx, sourceClient)
			if err != nil {
				log.Printf("Error: Failed to export users: %v\n", err)
				return
			}

			fmt.Printf("Export job started in source tenant. Job ID: %s\n", jobID)

			for {
				time.Sleep(10 * time.Second)
				location, err := checkJobStatus(ctx, sourceClient, jobID)
				if err == nil {
					fmt.Printf("Export completed. Download file at: %s\n", location)

					err = downloadFile(location, "exported_users.json.gz")
					if err != nil {
						log.Fatalf("Failed to download the file: %v", err)
					}
					break
				}
				fmt.Println("Export job not completed yet, checking again...")
			}
		},
	}

	var importCmd = &cobra.Command{
		Use:   "import",
		Short: "Import users into the target Auth0 tenant after splitting into chunks",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Println("Starting user import into target tenant...")

			file, err := os.Open("exported_users.json.gz")
			if err != nil {
				log.Printf("Failed to open JSON file: %v", err)
				return
			}
			defer file.Close()

			jsonData, err := unzipGZFile("exported_users.json.gz")
			if err != nil {
				log.Printf("Error: Failed to unzip the file: %v\n", err)
				return
			}

			reader := strings.NewReader(string(jsonData))
			chunks, err := splitJSONData(reader, 500000, true) // 500KB size chunks
			if err != nil {
				log.Printf("Error: Failed to split JSON data: %v\n", err)
				return
			}

			for i, chunk := range chunks {
				fmt.Printf("Importing chunk %d/%d...\n", i+1, len(chunks))
				err := importUsersChunk(ctx, targetClient, chunk)
				if err != nil {
					log.Fatalf("Failed to import chunk %d: %v", i+1, err)
				}
				fmt.Printf("Chunk %d imported successfully.\n", i+1)
			}

			fmt.Println("All chunks imported successfully into the target tenant.")
		},
	}

	rootCmd.AddCommand(exportCmd, importCmd)
	if err := rootCmd.Execute(); err != nil {
		log.Printf("Error: Command execution failed: %v\n", err)
	}
}
