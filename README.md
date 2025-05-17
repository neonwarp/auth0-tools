# Auth0 Tools

This is a Go CLI application that exports users from an Auth0 source tenant, downloads the exported file, splits the user data into 5KB chunks, and imports them sequentially into a target Auth0 tenant.

## Features

- Export users from a source Auth0 tenant.
- Download and unzip the exported .gz file.
- Split the user data into 5KB-sized chunks.
- Sequentially import each chunk into a target Auth0 tenant, waiting for each batch to complete before proceeding to the next.

## Requirements

- [Go](https://go.dev/) 1.24 or later
- [Auth0 Management API](https://auth0.com/docs/api/management/v2) credentials for both source and target tenants.
- Environment variables set up in a `.env` file.

## Installation

Clone the repository:

```bash
git clone https://github.com/neonwarp/auth0-tools.git
cd auth0-tools
```

### Install dependencies:

The application relies on the following Go modules:

- auth0/go-auth0: Auth0 SDK for Go
- spf13/cobra: For CLI handling
- joho/godotenv: For loading environment variables from a .env file

You can install the required dependencies using `go mod tidy`:

```bash
go mod tidy
```

### Set up the `.env` file:

Create a `.env` file in the root of your project with the following content:

```bash
# Source Auth0 Tenant
SOURCE_DOMAIN=your-source-auth0-domain
SOURCE_CLIENT_ID=your-source-auth0-client-id
SOURCE_CLIENT_SECRET=your-source-auth0-client-secret
SOURCE_CONNECTION_ID=your-source-connection-id

# Target Auth0 Tenant
DESTINATION_DOMAIN=your-target-auth0-domain
DESTINATION_CLIENT_ID=your-target-auth0-client-id
DESTINATION_CLIENT_SECRET=your-target-auth0-client-secret
DESTINATION_CONNECTION_ID=your-target-connection-id
```

Replace the placeholders with your actual Auth0 credentials.

## Usage

The CLI has two main commands: `export` and `import`.

### Export Users

This command exports users from the source Auth0 tenant, downloads the exported file, and saves it locally as exported_users.json.gz.

```bash
go run main.go export
```

### Import Users in Chunks

This command unzips the `exported_users.json.gz` file, splits the JSON into 5KB-sized chunks, and imports each chunk into the target Auth0 tenant. It waits for each batch to complete before proceeding to the next one.

```bash
go run main.go import
```
