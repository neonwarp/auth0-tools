package main

import (
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

func getSourceAuth0Client(ctx context.Context) (*management.Management, error) {
	domain := os.Getenv("SOURCE_DOMAIN")
	clientID := os.Getenv("SOURCE_CLIENT_ID")
	clientSecret := os.Getenv("SOURCE_CLIENT_SECRET")

	if domain == "" || clientID == "" || clientSecret == "" {
		return nil, fmt.Errorf("source Auth0 credentials are missing. Please check your .env file")
	}

	return management.New(domain, management.WithClientCredentials(ctx, clientID, clientSecret))
}

func getTargetAuth0Client(ctx context.Context) (*management.Management, error) {
	domain := os.Getenv("DESTINATION_DOMAIN")
	clientID := os.Getenv("DESTINATION_CLIENT_ID")
	clientSecret := os.Getenv("DESTINATION_CLIENT_SECRET")

	if domain == "" || clientID == "" || clientSecret == "" {
		return nil, fmt.Errorf("target Auth0 credentials are missing. Please check your .env file")
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

	var result strings.Builder
	_, err = io.Copy(&result, gzReader)
	if err != nil {
		return nil, fmt.Errorf("failed to read gz file: %w", err)
	}

	return []byte(result.String()), nil
}

func splitJSONData(data []byte, maxChunkSize int) ([][]map[string]interface{}, error) {
	var chunks [][]map[string]interface{}
	chunk := []map[string]interface{}{}
	chunkSize := 0

	lines := strings.Split(string(data), "\n")

	for _, line := range lines {
		line = strings.TrimSpace(line) // Remove leading/trailing whitespace
		if line == "" {
			continue // Skip empty lines
		}

		var user map[string]interface{}
		err := json.Unmarshal([]byte(line), &user)
		if err != nil {
			return nil, fmt.Errorf("failed to parse JSON: %w", err)
		}

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
	for {
		job, err := m.Job.Read(ctx, jobID)
		if err != nil {
			return fmt.Errorf("failed to read job status: %w", err)
		}

		if *job.Status == "completed" {
			fmt.Printf("Import job %s completed successfully.\n", jobID)
			return nil
		}

		if *job.Status == "failed" {
			return fmt.Errorf("import job %s failed", jobID)
		}

		fmt.Printf("Import job %s still in progress. Waiting...\n", jobID)
		time.Sleep(10 * time.Second)
	}
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
		log.Fatalf("Error loading .env file")
	}

	ctx := context.Background()

	sourceClient, err := getSourceAuth0Client(ctx)
	if err != nil {
		log.Fatalf("Failed to create Auth0 source client: %v", err)
	}

	targetClient, err := getTargetAuth0Client(ctx)
	if err != nil {
		log.Fatalf("Failed to create Auth0 target client: %v", err)
	}

	var rootCmd = &cobra.Command{Use: "auth0-cli"}

	var exportCmd = &cobra.Command{
		Use:   "export",
		Short: "Export users from the source Auth0 tenant",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Println("Starting user export from source tenant...")

			jobID, err := exportUsers(ctx, sourceClient)
			if err != nil {
				log.Fatalf("Failed to export users: %v", err)
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

			jsonData, err := unzipGZFile("exported_users.json.gz")
			if err != nil {
				log.Fatalf("Failed to unzip the file: %v", err)
			}

			chunks, err := splitJSONData(jsonData, 500000) // 500KB size chunks
			if err != nil {
				log.Fatalf("Failed to split the JSON data: %v", err)
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
	rootCmd.Execute()
}
