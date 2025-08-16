// *************************************************************************************************
// jira-spillover-get.go by Andrew Newbury, 2025-08-16
//
//		Purpose: Returns Jira issues (except epics, risks, and sub-tasks) for the user specified project
//	          that have been modified within a user defined number of days that have also been worked
//	          on in more than one sprint. Displays results to screen and exports to tab separated
//	          text file for importing and manipulation by Excel or similar tools.
//
// Features: Command line interface with interactive fallback for missing parameters
//
//	        Token file authentication support with Base64 encoding
//	        Configurable Jira base URL via command line or interactive prompt
//	        Days prior filtering for modified issues
//	        Multi-sprint issue identification and tracking
//	        Epic summary lookups with batch processing
//	        Comprehensive logging to both console and log file
//	        Progress tracking for large issue sets with batch processing
//	        Project validation before processing
//	        Interactive parameter input with validation
//
//	Output: Tab-separated text file with Jira issues that have been worked on in multiple sprints
//	        Logs all activity to timestamped log file
//
// Example usage, see function showUsage for details:
//
//	.\jira-spillover-get.exe [-TokenFile token_file_path] [-url jira_base_url] [-project project_key] [-fromdate yyyy-mm-dd] [-daysprior #] [-outputfile filename] [-pair customfield_10186] [-append] [-log] [-debug][-? | /? | --help | -help]
//
//	 With no supplied command line parameters, you will be prompted interactively.
//
// Authentication: Uses HTTP Basic Authentication with Base64 encoded credentials
//
//	Token file format: username:api-token (single line)
//	Supports both command line token file specification and interactive prompt
//
// API Endpoints:
//
//	/rest/api/2/project/{projectKey} - Validates project exists and user has access
//	/rest/api/2/search - Retrieves issues matching JQL query with pagination
//	/rest/api/2/issue/{issueKey} - Retrieves epic title information
//
// History (update version string on line ~95):
//	0.1.3 FIX lookup and return epic summaries, not epic titles; FIX Pair counter
//	0.1.2 FIX extract sprint names from both arrays of maps (the current Jira API format) and legacy string formats
//  0.1.1 updated default custom names for out-of-the-box Jira Cloud (except Pair, which is a custom added field)
//  0.1.0 pair field provied optionally va command line
//  0.0.9 Changed "Sub Task" to "Sub-Task" to match Cloud out of box configuration
//  0.0.8 FIX golangci-lint recommendations (testing returns from resp.Body.Close(), file.Close, and logFile.Close))
//	0.0.7 updated README.md, version bump
//	0.0.6 updated sample prompts
//	0.0.5 cosmetic source code changes
//	0.0.4 updated help output
//	0.0.3 added "components" field to output, execution time dispalyed on exit, output defaults to .tsv
//	0.0.2 added optional append to output file
//	0.0.1 initial Go port from PowerShell jira-spillover-chart-data.ps1 v0.1.15
//		  converted PowerShell script to Go using jira-calc-sp.go as template
//		  maintained all core functionality including logging and multi-sprint detection
//		  added pagination for large result sets
//		  added epic title lookups with batch processing
//
// To do:
//
//	...
//
// *************************************************************************************************
// Copyright (C) 2025 Andrew Newbury
//
// This program is free software: you can redistribute it and/or modify it under the terms of the
// GNU General Public License as published by the Free Software Foundation, either version 3 of
// the License, or (at your option) any later version.
//
// This program is distributed in the hope that it will be useful, but WITHOUT ANY WARRANTY;
// without even the implied warranty of MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See
// the GNU General Public License for more details.
//
// You should have received a copy of the GNU General Public License along with this program.
// If not, see <https://www.gnu.org/licenses/>.
// *************************************************************************************************
//go:generate goversioninfo
// *************************************************************************************************

package main

import (
	"bufio"           // For reading user input from stdin
	"encoding/base64" // For Base64 encoding of authentication credentials
	"encoding/json"   // For parsing JSON responses from Jira API
	"fmt"             // For formatted printing and string formatting
	"io"              // For reading HTTP response bodies
	"log"             // For logging to file
	"net/http"        // For making HTTP requests to Jira API
	"net/url"         // For URL encoding JQL queries
	"os"              // For command line arguments and file operations
	"regexp"          // For parsing sprint field values
	"strconv"         // For string to number conversion
	"strings"         // For string manipulation and processing
	"time"            // For date validation and timestamp formatting
)

// Program metadata - update these values when changing the program
const (
	programName    = "jira-spillover-get"
	programVersion = "0.1.3"
)

// Default configuration constants
const (
	defaultStoryPointsField = "customfield_10059" // Default story points field
	defaultSprintField      = "customfield_10020" // Default sprint field
	defaultEpicLinkField    = "customfield_10014" // Default epic link field
	batchSize               = 100                 // Number of issues to fetch per API call
	defaultDaysPrior        = 10                  // Default number of days to look back
)

// IssueFields represents the fields section of a Jira issue
type IssueFields struct {
	IssueType        IssueType                  `json:"issuetype"`         // Issue type information
	Status           Status                     `json:"status"`            // Issue status information
	Summary          string                     `json:"summary"`           // Issue summary/title
	Updated          *string                    `json:"updated"`           // Last updated date
	Created          *string                    `json:"created"`           // Creation date
	ResolutionDate   *string                    `json:"resolutiondate"`    // Resolution date (can be null)
	Assignee         *Assignee                  `json:"assignee"`          // Current assignee (can be null)
	Creator          *Creator                   `json:"creator"`           // Issue creator
	Project          Project                    `json:"project"`           // Project information
	FixVersions      []FixVersion               `json:"fixVersions"`       // Target release versions
	Components       []Component                `json:"components"`        // Issue components
	Labels           []string                   `json:"labels"`            // Issue labels
	Resolution       *Resolution                `json:"resolution"`        // Resolution status (can be null)
	StoryPoints      interface{}                `json:"customfield_10059"` // Story points (can be various types)
	SprintField      interface{}                `json:"customfield_10020"` // Sprint field (can be array or null)
	EpicLinkField    interface{}                `json:"customfield_10014"` // Epic link (can be string or null)
	AdditionalFields map[string]json.RawMessage `json:"-"`                 // holds raw JSON for any fields not mapped above (including dynamic custom fields)
}

// Issue represents a Jira issue from the search API response
type Issue struct {
	Key    string      `json:"key"`    // Issue key (e.g., "EXPD-1234")
	Fields IssueFields `json:"fields"` // Issue field data
}

// UnmarshalJSON implements custom unmarshalling to capture both known fields and any additional custom fields
func (f *IssueFields) UnmarshalJSON(data []byte) error {
	// Define an alias type to avoid recursion
	type Alias IssueFields
	aux := &struct {
		*Alias
	}{Alias: (*Alias)(f)}

	// First unmarshal into a generic map to capture raw fields
	var rawMap map[string]json.RawMessage
	if err := json.Unmarshal(data, &rawMap); err != nil {
		return err
	}

	// Unmarshal known fields from the raw map into aux
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}

	// Store any additional fields (those not represented by the struct tags above)
	f.AdditionalFields = make(map[string]json.RawMessage)
	// Known keys from IssueFields tags
	knownKeys := map[string]bool{
		"issuetype":         true,
		"status":            true,
		"summary":           true,
		"updated":           true,
		"created":           true,
		"resolutiondate":    true,
		"assignee":          true,
		"creator":           true,
		"project":           true,
		"fixVersions":       true,
		"components":        true,
		"labels":            true,
		"resolution":        true,
		"customfield_10002": true,
		"customfield_14181": true,
		"customfield_14182": true,
		// Do NOT include customfield_10186 (Pair) so it is added to AdditionalFields
	}

	for k, v := range rawMap {
		if !knownKeys[k] {
			f.AdditionalFields[k] = v
		}
	}

	return nil
}

// IssueType represents issue type information
type IssueType struct {
	Name string `json:"name"` // Issue type name (Story, Task, Bug, etc.)
}

// Status represents issue status information
type Status struct {
	Name string `json:"name"` // Status name (Closed, Story Done, etc.)
}

// Assignee represents assignee information
type Assignee struct {
	DisplayName string `json:"displayName"` // Assignee display name
}

// Creator represents creator information
type Creator struct {
	DisplayName string `json:"displayName"` // Creator display name
}

// Project represents project information
type Project struct {
	Key  string `json:"key"`  // Project key
	Name string `json:"name"` // Project name
}

// FixVersion represents fix version information
type FixVersion struct {
	Name string `json:"name"` // Version name
}

// Component represents component information
type Component struct {
	Name string `json:"name"` // Component name
}

// Resolution represents resolution information
type Resolution struct {
	Name string `json:"name"` // Resolution name
}

// PairMember represents pair programming member information
type PairMember struct {
	DisplayName string `json:"displayName"` // Pair member display name
}

// SearchResponse represents the response from Jira's search API
type SearchResponse struct {
	Issues     []Issue `json:"issues"`     // Array of issues
	Total      int     `json:"total"`      // Total number of matching issues
	StartAt    int     `json:"startAt"`    // Starting index for this batch
	MaxResults int     `json:"maxResults"` // Maximum results per batch
}

// ProjectInfo represents basic project information for validation
type ProjectInfo struct {
	Key  string `json:"key"`  // Project key
	Name string `json:"name"` // Project name
}

// EpicInfo represents epic information for title lookups
type EpicInfo struct {
	Key    string           `json:"key"`    // Epic key
	Fields EpicFieldsLookup `json:"fields"` // Epic fields for title lookup
}

// EpicFieldsLookup represents epic fields for title lookup
// Only include Summary for epic title
type EpicFieldsLookup struct {
	Summary string `json:"summary"` // Epic summary/title
}

// SprintInfo represents parsed sprint information
type SprintInfo struct {
	SprintCount int      // Number of sprints issue has been in
	SprintNames []string // List of unique sprint names
	FirstSprint string   // Name of first sprint
	LastSprint  string   // Name of last sprint
	AllSprints  string   // Comma-separated list of all sprint names
}

// MultisprintIssue represents an issue that has been in multiple sprints
type MultisprintIssue struct {
	Issue         Issue      // The original issue data
	WorkedSprints int        // Number of sprints worked
	EpicLink      string     // Epic key or "No Epic"
	ResolvedDate  *time.Time // When issue was resolved (if applicable)
	SprintInfo    SprintInfo // Sprint information for the issue
}

// Global variables for logging
var (
	logFile           *os.File
	logger            *log.Logger
	startTime         time.Time
	enableLogging     bool   // Add flag to control logging
	enableDebug       bool   // Add flag to control debug output
	pairFieldName     string // pairFieldName is the JSON field name to look up for Pair information when provided
	pairFieldProvided bool   // pairFieldProvided is true when the -Pair command line switch was provided
)

/********************************************************************************************************************************/
// initLogging initializes the logging system with timestamped log file (only if logging is enabled)
//
// Creates a log file with timestamp in the filename and sets up both console and file logging.
// The log file format is: jira-spillover-get-YYYYMMDD-HHMMSS.log
//
// Returns:
//   error - any error encountered during log file creation
//
// Side effects:
//   - Creates a new log file in the current directory (only if enableLogging is true)
//   - Sets global logger variable for use throughout the program
//   - Registers cleanup function to close log file on program exit
func initLogging() error {
	// Record start time for execution duration calculation in cleanup()
	startTime = time.Now()

	// Only create log file if logging is enabled
	if !enableLogging {
		return nil
	}

	// Generate timestamp in Go's reference time format (Mon Jan 2 15:04:05 MST 2006)
	// This creates a compact format: YYYYMMDD-HHMMSS
	timestamp := startTime.Format("20060102-150405")

	// Build log filename with timestamp to ensure unique files per execution
	logFileName := fmt.Sprintf("%s-%s.log", programName, timestamp)

	// Create the log file in the current working directory
	// This will overwrite any existing file with the same name (unlikely due to timestamp)
	var err error
	logFile, err = os.Create(logFileName)
	if err != nil {
		return fmt.Errorf("failed to create log file: %w", err)
	}

	// Create a new logger that writes to the file
	// Empty prefix ("") and flags (0) mean we'll handle formatting in writeLog()
	logger = log.New(logFile, "", 0)

	// Log the initial startup message to both console and file
	writeLog("INFO", fmt.Sprintf("Starting %s v%s", programName, programVersion))
	writeLog("INFO", fmt.Sprintf("Log file: %s", logFileName))

	return nil
}

/********************************************************************************************************************************/
// writeLog writes a log message to both console and log file
//
// Formats messages with timestamp and log level, displaying appropriate colors
// for different log levels on the console while writing plain text to the log file.
//
// Parameters:
//   level   - log level ("INFO", "WARNING", "ERROR")
//   message - message to log
//
// Side effects:
//   - Prints formatted message to stdout with appropriate coloring
//   - Writes plain text message to log file with timestamp (only if logging is enabled)
func writeLog(level, message string) {
	// Create timestamp for consistent formatting
	timestamp := time.Now().Format("2006-01-02 15:04:05")
	logMessage := fmt.Sprintf("[%s] [%s] %s", timestamp, level, message)

	// Print to console with appropriate colors based on log level
	switch level {
	case "INFO":
		fmt.Println(logMessage) // Default color for info
	case "WARNING":
		fmt.Printf("\033[33m%s\033[0m\n", logMessage) // Yellow for warnings
	case "ERROR":
		fmt.Printf("\033[31m%s\033[0m\n", logMessage) // Red for errors
	default:
		fmt.Println(logMessage) // Default color for unknown levels
	}

	// Write to log file if logging is enabled
	if enableLogging && logger != nil {
		logger.Println(logMessage)
	}
}

/********************************************************************************************************************************/
// readTokenFile reads the Jira API token from the specified file
// Token file should contain "username:api-token" format on a single line
//
// The function performs several validation steps:
// 1. Checks if the file exists and is accessible
// 2. Reads the file content safely into memory
// 3. Validates the format (must contain a colon separator)
// 4. Encodes the credentials as Base64 for HTTP Basic Authentication
//
// Parameters:
//   tokenFilePath - absolute or relative path to the token file
//
// Returns:
//   string - Base64 encoded token string for use in Authorization headers
//   error  - any error encountered during file reading or validation
//
// Example token file content:
//   username@company.com:abc123def456ghi789
func readTokenFile(tokenFilePath string) (string, error) {
	// Check if file exists
	if _, err := os.Stat(tokenFilePath); os.IsNotExist(err) {
		return "", fmt.Errorf("token file not found: %s", tokenFilePath)
	}

	// Read file content
	content, err := os.ReadFile(tokenFilePath)
	if err != nil {
		return "", fmt.Errorf("failed to read token file: %w", err)
	}

	// Convert to string and trim whitespace
	tokenString := strings.TrimSpace(string(content))

	// Validate that token is not empty
	if tokenString == "" {
		return "", fmt.Errorf("token file is empty")
	}

	// Validate format (should contain colon separator)
	if !strings.Contains(tokenString, ":") {
		writeLog("WARNING", "API token might not be in expected format (username:token)")
	}

	// Encode as Base64 for HTTP Basic Authentication
	encoded := base64.StdEncoding.EncodeToString([]byte(tokenString))
	writeLog("INFO", "Successfully read and encoded API token")

	return encoded, nil
}

/***********************************************************************************************************************************/
// getJiraBaseURL gets the Jira base URL from command line arguments or prompts user
//
// This function implements a flexible URL acquisition strategy:
// 1. First checks command line arguments for -url or -URL parameter (case-insensitive)
// 2. If found, validates and uses the provided URL
// 3. If not found, prompts the user interactively for the URL
// 4. Performs URL cleanup by removing trailing slashes
// 5. Validates that a URL was provided (exits program if empty)
//
// Parameters: None (reads from os.Args and stdin)
//
// Returns:
//   string - validated Jira base URL (without trailing slash)
//
// Side effects:
//   - May prompt user for input via stdin
//   - May call os.Exit(1) if URL validation fails
//   - Prints status messages to stdout
func getJiraBaseURL() string {
	// Check command line arguments for URL parameter
	args := os.Args[1:]
	for i, arg := range args {
		if strings.ToLower(arg) == "-url" && i+1 < len(args) {
			url := strings.TrimSpace(args[i+1])
			if url != "" {
				writeLog("INFO", fmt.Sprintf("Using Jira base URL from command line: %s", url))
				return strings.TrimRight(url, "/")
			}
		}
	}

	// Prompt user for URL if not found in command line
	fmt.Print("Enter the Jira base URL (e.g., https://jira.company.com): ")
	scanner := bufio.NewScanner(os.Stdin)
	if scanner.Scan() {
		url := strings.TrimSpace(scanner.Text())
		if url == "" {
			writeLog("ERROR", "Jira base URL is required")
			os.Exit(1)
		}
		writeLog("INFO", fmt.Sprintf("Using Jira base URL from user input: %s", url))
		return strings.TrimRight(url, "/")
	}

	writeLog("ERROR", "Failed to read Jira base URL")
	os.Exit(1)
	return ""
}

/***********************************************************************************************************************************/
// getAuthToken gets the authentication token from command line or prompts user
//
// This function implements a flexible authentication token acquisition strategy:
// 1. First checks command line arguments for -TokenFile or -tokenfile parameter (case-insensitive)
// 2. If found, attempts to read the specified token file
// 3. If not found, prompts the user interactively for the token file path
// 4. Delegates to readTokenFile() for actual file reading and validation
// 5. Returns the Base64 encoded token ready for HTTP Basic Authentication
//
// Parameters: None (reads from os.Args and stdin)
//
// Returns:
//   string - Base64 encoded authentication token
//   error  - any error from file reading, validation, or user input
//
// Side effects:
//   - May prompt user for input via stdin
//   - Prints status messages to stdout indicating token file source
func getAuthToken() (string, error) {
	// Check command line arguments for token file parameter
	args := os.Args[1:]
	for i, arg := range args {
		if strings.ToLower(arg) == "-tokenfile" && i+1 < len(args) {
			tokenFile := strings.TrimSpace(args[i+1])
			if tokenFile != "" {
				writeLog("INFO", fmt.Sprintf("Using token file from command line: %s", tokenFile))
				return readTokenFile(tokenFile)
			}
		}
	}

	// Prompt user for token file path if not found in command line
	fmt.Print("Enter the path to your Jira API token file: ")
	scanner := bufio.NewScanner(os.Stdin)
	if scanner.Scan() {
		tokenFile := strings.TrimSpace(scanner.Text())
		if tokenFile == "" {
			return "", fmt.Errorf("token file path is required")
		}
		writeLog("INFO", fmt.Sprintf("Using token file from user input: %s", tokenFile))
		return readTokenFile(tokenFile)
	}

	return "", fmt.Errorf("failed to read token file path")
}

/***********************************************************************************************************************************/
// getProjectFromCommandLine checks for -project parameter in command line arguments
//
// This function scans command line arguments for a -project parameter and returns
// the specified project code if found. Supports case-insensitive parameter matching.
//
// Parameters: None (reads from os.Args)
//
// Returns:
//   string - project code from command line (uppercase), or empty string if not found
//
// Side effects:
//   - Prints status message if project parameter is found
func getProjectFromCommandLine() string {
	args := os.Args[1:]
	for i, arg := range args {
		if strings.ToLower(arg) == "-project" && i+1 < len(args) {
			project := strings.TrimSpace(strings.ToUpper(args[i+1]))
			if project != "" {
				writeLog("INFO", fmt.Sprintf("Using project key from command line: %s", project))
				return project
			}
		}
	}
	return ""
}

/***********************************************************************************************************************************/
// getDateAndDaysFromCommandLine checks for date and days prior parameters in command line arguments
//
// This function scans command line arguments for -fromdate and -daysprior parameters
// and returns them if found. Supports case-insensitive parameter matching.
//
// Parameters: None (reads from os.Args)
//
// Returns:
//   fromDate - from date from command line (yyyy-mm-dd format), or empty string if not found
//   daysPrior - days prior from command line, or 0 if not found
//   fromDateProvided - true if fromdate parameter was explicitly provided on command line
//   daysPriorProvided - true if daysprior parameter was explicitly provided on command line
//
// Side effects:
//   - Prints status messages if parameters are found
func getDateAndDaysFromCommandLine() (string, int, bool, bool) {
	args := os.Args[1:]
	var fromDate string
	var daysPrior int
	var fromDateProvided, daysPriorProvided bool

	for i, arg := range args {
		switch strings.ToLower(arg) {
		case "-fromdate":
			if i+1 < len(args) {
				fromDate = strings.TrimSpace(args[i+1])
				if fromDate != "" {
					fromDateProvided = true
					writeLog("INFO", fmt.Sprintf("Using from date from command line: %s", fromDate))
				}
			}
		case "-daysprior":
			if i+1 < len(args) {
				if days, err := strconv.Atoi(strings.TrimSpace(args[i+1])); err == nil {
					daysPrior = days
					daysPriorProvided = true
					writeLog("INFO", fmt.Sprintf("Using days prior from command line: %d", daysPrior))
				}
			}
		}
	}

	return fromDate, daysPrior, fromDateProvided, daysPriorProvided
}

/***********************************************************************************************************************************/
// getOutputFileFromCommandLine checks for -outputfile parameter in command line arguments
//
// This function scans command line arguments for a -outputfile parameter and returns
// the specified filename if found. Supports case-insensitive parameter matching.
//
// Parameters: None (reads from os.Args)
//
// Returns:
//   string - output filename from command line, or empty string if not found
//
// Side effects:
//   - Prints status message if parameter is found
func getOutputFileFromCommandLine() string {
	args := os.Args[1:]
	for i, arg := range args {
		if strings.ToLower(arg) == "-outputfile" && i+1 < len(args) {
			outputFile := strings.TrimSpace(args[i+1])
			if outputFile != "" {
				writeLog("INFO", fmt.Sprintf("Using output file from command line: %s", outputFile))
				return outputFile
			}
		}
	}
	return ""
}

/***********************************************************************************************************************************/
// getAppendFlagFromCommandLine checks for -append parameter in command line arguments
//
// This function scans command line arguments for a -append parameter and returns
// true if found, enabling append mode for output file.
//
// Parameters: None (reads from os.Args)
//
// Returns:
//   bool - true if -append flag is present, false otherwise
//
// Side effects:
//   - Prints status message if append flag is found
func getAppendFlagFromCommandLine() bool {
	args := os.Args[1:]
	for _, arg := range args {
		if strings.ToLower(arg) == "-append" {
			writeLog("INFO", "Append mode enabled from command line")
			return true
		}
	}
	return false
}

/***********************************************************************************************************************************/
// getDebugFlagFromCommandLine checks for -debug parameter in command line arguments
func getDebugFlagFromCommandLine() bool {
	args := os.Args[1:]
	for _, arg := range args {
		if strings.ToLower(arg) == "-debug" {
			fmt.Println("Debug output enabled from command line")
			return true
		}
	}
	return false
}

/***********************************************************************************************************************************/
// getProjectKeyInteractively prompts the user to enter a project key
//
// This function provides an interactive way to specify a project key if not provided
// via command line. Validates that a non-empty project key is entered.
//
// Parameters: None
//
// Returns:
//   string - project key from user input (uppercase)
//   error  - any error encountered during user input reading
//
// Side effects:
//   - Prompts user for input via stdin
//   - Prints status message when project key is entered
func getProjectKeyInteractively() (string, error) {
	fmt.Print("Enter the Jira Project ID (e.g., EXPD): ")
	scanner := bufio.NewScanner(os.Stdin)
	if scanner.Scan() {
		projectKey := strings.TrimSpace(strings.ToUpper(scanner.Text()))
		if projectKey == "" {
			return "", fmt.Errorf("project key is required")
		}
		writeLog("INFO", fmt.Sprintf("Using project key from user input: %s", projectKey))
		return projectKey, nil
	}
	return "", fmt.Errorf("failed to read project key")
}

/***********************************************************************************************************************************/
// getDateRangeInteractively prompts the user to enter date range parameters
//
// This function provides an interactive way to specify date range if not provided
// via command line. Allows user to specify either a from date or days prior.
//
// Parameters: None
//
// Returns:
//   fromDate - from date from user input (yyyy-mm-dd format), or empty string if not provided
//   daysPrior - days prior from user input, or default value if not provided
//   error - any error encountered during user input reading
//
// Side effects:
//   - Prompts user for input via stdin
//   - Prints status messages when parameters are entered or left blank
func getDateRangeInteractively() (string, int, error) {
	fmt.Print("Enter a specific date to check from (yyyy-mm-dd), or leave blank: ")
	scanner := bufio.NewScanner(os.Stdin)
	if scanner.Scan() {
		fromDate := strings.TrimSpace(scanner.Text())
		if fromDate != "" {
			writeLog("INFO", fmt.Sprintf("Using from date from user input: %s", fromDate))
			return fromDate, 0, nil
		}
	}

	fmt.Printf("Enter number of days prior to check from (default = %d): ", defaultDaysPrior)
	if scanner.Scan() {
		daysInput := strings.TrimSpace(scanner.Text())
		if daysInput != "" {
			if days, err := strconv.Atoi(daysInput); err == nil && days > 0 {
				writeLog("INFO", fmt.Sprintf("Using days prior from user input: %d", days))
				return "", days, nil
			} else {
				writeLog("WARNING", fmt.Sprintf("Invalid input '%s'. Using default of %d days", daysInput, defaultDaysPrior))
			}
		}
	}

	writeLog("INFO", fmt.Sprintf("Using default days prior: %d", defaultDaysPrior))
	return "", defaultDaysPrior, nil
}

/***********************************************************************************************************************************/
// getOutputFileInteractively prompts the user to enter an output filename
//
// This function provides an interactive way to specify output filename if not provided
// via command line. Provides a default filename if none is entered.
//
// Parameters: None
//
// Returns:
//   string - output filename from user input, or default if not provided
//   error - any error encountered during user input reading
//
// Side effects:
//   - Prompts user for input via stdin
//   - Prints status message when filename is entered or default is used
func getOutputFileInteractively() (string, error) {
	fmt.Print("Enter the filename to save the results (default *overwrites* spillover_rpt.tsv): ")
	scanner := bufio.NewScanner(os.Stdin)
	if scanner.Scan() {
		outputFile := strings.TrimSpace(scanner.Text())
		if outputFile != "" {
			writeLog("INFO", fmt.Sprintf("Using output file from user input: %s", outputFile))
			return outputFile, nil
		}
	}

	defaultFile := "spillover_rpt.tsv"
	writeLog("INFO", fmt.Sprintf("Using default output file: %s", defaultFile))
	return defaultFile, nil
}

/***********************************************************************************************************************************/
// validateDate validates a date string in yyyy-MM-dd format
//
// This function checks if the provided date string matches the expected format
// and represents a valid date.
//
// Parameters:
//   dateStr - date string to validate
//   fieldName - name of the field being validated (for error messages)
//
// Returns:
//   error - validation error, or nil if valid
func validateDate(dateStr, fieldName string) error {
	if dateStr == "" {
		return nil // Empty dates are allowed
	}

	// Parse date using strict format
	_, err := time.Parse("2006-01-02", dateStr)
	if err != nil {
		return fmt.Errorf("invalid %s format '%s': must be yyyy-mm-dd", fieldName, dateStr)
	}

	return nil
}

/***********************************************************************************************************************************/
// validateProject checks if a Jira project exists and is accessible
//
// This function makes an HTTP GET request to the project endpoint to verify
// that the project exists and the user has permission to view it.
//
// Parameters:
//   jiraBaseURL - base URL of the Jira instance
//   authToken   - Base64 encoded authentication token
//   projectKey  - project key to validate
//
// Returns:
//   error - any error if project doesn't exist or isn't accessible, nil if valid
//
// Side effects:
//   - Makes HTTP request to Jira API
//   - Writes log messages about validation results
func validateProject(jiraBaseURL, authToken, projectKey string) error {
	// Build project validation URL
	projectURL := fmt.Sprintf("%s/rest/api/2/project/%s", jiraBaseURL, projectKey)

	// Create HTTP request
	req, err := http.NewRequest("GET", projectURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create project validation request: %w", err)
	}

	// Set authentication header
	req.Header.Set("Authorization", "Basic "+authToken)
	req.Header.Set("Accept", "application/json")

	// Make HTTP request
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to validate project: %w", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			log.Printf("failed to close response body: %v", err)
		}
	}()

	// Check response status
	if resp.StatusCode == 404 {
		return fmt.Errorf("project '%s' does not exist (HTTP 404 Not Found)", projectKey)
	} else if resp.StatusCode != 200 {
		return fmt.Errorf("failed to validate project '%s' (HTTP %d)", projectKey, resp.StatusCode)
	}

	// Parse response to validate project data
	var projectInfo ProjectInfo
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read project validation response: %w", err)
	}

	if err := json.Unmarshal(body, &projectInfo); err != nil {
		return fmt.Errorf("failed to parse project validation response: %w", err)
	}

	// Validate that the project key matches
	if projectInfo.Key != projectKey {
		return fmt.Errorf("project key mismatch: expected '%s', got '%s'", projectKey, projectInfo.Key)
	}

	writeLog("INFO", fmt.Sprintf("Project '%s' found: %s", projectKey, projectInfo.Name))
	return nil
}

/***********************************************************************************************************************************/
// buildJQLQuery constructs a JQL (Jira Query Language) query string for retrieving spillover issues
//
// This function creates a properly formatted JQL query to filter issues based on:
// - Project key (required)
// - Issue types (excludes Epic, Risk, Sub Task)
// - Sprint field is not empty (only issues that have been in sprints)
// - Updated date range (based on days prior)
//
// Parameters:
//   projectKey - the Jira project key to filter by (e.g., "PROJ", "TEAM")
//   daysPrior  - number of days to look back for updated issues
//
// Returns:
//   string - complete JQL query ready for use with Jira REST API
//
// Side effects:
//   - Writes log message with the constructed JQL query for debugging and audit purposes
func buildJQLQuery(projectKey string, daysPrior int) string {
	// Build JQL query to find spillover candidates
	// Excludes Epics, Risks, and Sub Tasks
	// Only includes issues with Sprint field populated
	// Only includes issues updated within the specified time frame
	jqlQuery := fmt.Sprintf("project = %s AND issuetype not in (Epic, Risk, 'Sub-Task') AND Sprint is not EMPTY AND updated >= -%dd",
		projectKey, daysPrior)

	writeLog("INFO", fmt.Sprintf("Using JQL query: %s", jqlQuery))
	return jqlQuery
}

/***********************************************************************************************************************************/
// fetchAllJiraIssues retrieves all issues matching the JQL query using pagination
//
// This function handles Jira's REST API pagination limits by making multiple requests
// to fetch all matching issues. Jira typically limits responses to 50-100 issues per
// request for performance reasons, so this function automatically handles batching.
//
// Parameters:
//   jiraBaseURL - base URL of the Jira instance
//   authToken   - Base64 encoded authentication token
//   jqlQuery    - JQL query string to execute
//   fields      - comma-separated list of fields to retrieve
//
// Returns:
//   []Issue - slice containing all matching issues from all paginated requests
//   error   - any error encountered during fetching
//
// Side effects:
//   - Makes multiple HTTP requests to Jira REST API
//   - Prints progress messages to console for large result sets
//   - Writes detailed log messages about fetch progress and completion
func fetchAllJiraIssues(jiraBaseURL, authToken, jqlQuery, fields string) ([]Issue, error) {
	var allIssues []Issue
	startAt := 0
	batchCount := 0

	for {
		batchCount++
		writeLog("INFO", fmt.Sprintf("Fetching batch %d, starting at record %d...", batchCount, startAt))

		// Build URL with pagination parameters
		encodedJQL := url.QueryEscape(jqlQuery)
		requestURL := fmt.Sprintf("%s/rest/api/2/search?jql=%s&startAt=%d&maxResults=%d",
			jiraBaseURL, encodedJQL, startAt, batchSize)

		if fields != "" {
			requestURL += "&fields=" + url.QueryEscape(fields)
		}

		// Create HTTP request
		req, err := http.NewRequest("GET", requestURL, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to create request for batch %d: %w", batchCount, err)
		}

		// Set headers
		req.Header.Set("Authorization", "Basic "+authToken)
		req.Header.Set("Accept", "application/json")

		// Make HTTP request
		client := &http.Client{Timeout: 60 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch batch %d: %w", batchCount, err)
		}

		// Read response body
		body, err := io.ReadAll(resp.Body)
		if cerr := resp.Body.Close(); cerr != nil {
			writeLog("WARNING", fmt.Sprintf("failed to close response body: %v", cerr))
		}
		if err != nil {
			return nil, fmt.Errorf("failed to read response body for batch %d: %w", batchCount, err)
		}

		// Check HTTP status
		if resp.StatusCode != 200 {
			return nil, fmt.Errorf("HTTP %d error in batch %d: %s", resp.StatusCode, batchCount, string(body))
		}

		// Parse JSON response
		var searchResponse SearchResponse
		if err := json.Unmarshal(body, &searchResponse); err != nil {
			return nil, fmt.Errorf("failed to parse JSON response for batch %d: %w", batchCount, err)
		}

		// Add issues to collection
		allIssues = append(allIssues, searchResponse.Issues...)
		writeLog("INFO", fmt.Sprintf("Fetched %d issues (Total: %d/%d)",
			len(searchResponse.Issues), len(allIssues), searchResponse.Total))

		// Check if we've fetched all issues
		if startAt+batchSize >= searchResponse.Total {
			break
		}

		// Move to next batch
		startAt += batchSize
	}

	writeLog("INFO", fmt.Sprintf("Completed fetching %d issues in %d batches", len(allIssues), batchCount))
	return allIssues, nil
}

/***********************************************************************************************************************************/
// parseSprintField extracts sprint information from the Jira sprint field
//
// This function parses the sprint field which can contain multiple sprint objects
// and extracts sprint names and other information.
//
// Parameters:
//   sprintField - the sprint field value from Jira (can be array or null)
//
// Returns:
//   SprintInfo - parsed sprint information including names and counts
func parseSprintField(sprintField interface{}) SprintInfo {
	info := SprintInfo{
		SprintNames: []string{},
	}

	if sprintField == nil {
		return info
	}

	uniqueNames := make(map[string]bool)

	switch v := sprintField.(type) {
	case []interface{}:
		for _, sprint := range v {
			// Handle map[string]interface{} (Jira API format)
			if sprintMap, ok := sprint.(map[string]interface{}); ok {
				if nameVal, exists := sprintMap["name"]; exists {
					if sprintName, ok := nameVal.(string); ok {
						if !uniqueNames[sprintName] {
							uniqueNames[sprintName] = true
							info.SprintNames = append(info.SprintNames, sprintName)
						}
					}
				}
			} else if sprintStr, ok := sprint.(string); ok {
				// Fallback: handle string format (legacy)
				// Try to extract name=... from string
				nameRegex := regexp.MustCompile(`name=([^,]+)`)
				matches := nameRegex.FindStringSubmatch(sprintStr)
				if len(matches) > 1 {
					sprintName := matches[1]
					if !uniqueNames[sprintName] {
						uniqueNames[sprintName] = true
						info.SprintNames = append(info.SprintNames, sprintName)
					}
				}
			}
		}
	case []string:
		for _, sprintStr := range v {
			nameRegex := regexp.MustCompile(`name=([^,]+)`)
			matches := nameRegex.FindStringSubmatch(sprintStr)
			if len(matches) > 1 {
				sprintName := matches[1]
				if !uniqueNames[sprintName] {
					uniqueNames[sprintName] = true
					info.SprintNames = append(info.SprintNames, sprintName)
				}
			}
		}
	case string:
		nameRegex := regexp.MustCompile(`name=([^,]+)`)
		matches := nameRegex.FindStringSubmatch(v)
		if len(matches) > 1 {
			sprintName := matches[1]
			if !uniqueNames[sprintName] {
				uniqueNames[sprintName] = true
				info.SprintNames = append(info.SprintNames, sprintName)
			}
		}
	}

	// Set sprint information
	info.SprintCount = len(info.SprintNames)
	if len(info.SprintNames) > 0 {
		info.FirstSprint = info.SprintNames[0]
		info.LastSprint = info.SprintNames[len(info.SprintNames)-1]
		info.AllSprints = strings.Join(info.SprintNames, ", ")
	}

	return info
}

/***********************************************************************************************************************************/
// getEpicLink safely extracts epic link from issue fields
//
// Parameters:
//   epicLinkField - the epic link field value from Jira
//
// Returns:
//   string - epic key or "No Epic" if not found
func getEpicLink(epicLinkField interface{}) string {
	if epicLinkField == nil {
		return "No Epic"
	}

	if epicKey, ok := epicLinkField.(string); ok && epicKey != "" {
		return epicKey
	}

	return "No Epic"
}

/***********************************************************************************************************************************/
// fetchEpicTitles retrieves epic summaries for the given epic keys
//
// This function makes API calls to fetch epic summary information for multiple epics.
//
// Parameters:
//   jiraBaseURL - base URL of the Jira instance
//   authToken   - Base64 encoded authentication token
//   epicKeys    - slice of epic keys to look up
//
// Returns:
//   map[string]string - mapping of epic key to epic summary
//   error - any error encountered during fetching
func fetchEpicTitles(jiraBaseURL, authToken string, epicKeys []string) (map[string]string, error) {
	epicTitles := make(map[string]string)

	if len(epicKeys) == 0 {
		return epicTitles, nil
	}

	writeLog("INFO", fmt.Sprintf("Looking up %d unique Epic titles", len(epicKeys)))

	for i, epicKey := range epicKeys {
		writeLog("INFO", fmt.Sprintf("Looking up Epic summary %d of %d: %s", i+1, len(epicKeys), epicKey))

		// Build epic lookup URL: request only summary
		epicURL := fmt.Sprintf("%s/rest/api/2/issue/%s?fields=summary", jiraBaseURL, epicKey)

		// Create HTTP request
		req, err := http.NewRequest("GET", epicURL, nil)
		if err != nil {
			writeLog("WARNING", fmt.Sprintf("Failed to create request for Epic %s: %v", epicKey, err))
			epicTitles[epicKey] = "Epic Summary Lookup Failed"
			continue
		}

		// Set headers
		req.Header.Set("Authorization", "Basic "+authToken)
		req.Header.Set("Accept", "application/json")

		// Make HTTP request
		client := &http.Client{Timeout: 30 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			writeLog("WARNING", fmt.Sprintf("Failed to lookup Epic %s: %v", epicKey, err))
			epicTitles[epicKey] = "Epic Summary Lookup Failed"
			continue
		}

		// Read response body
		body, err := io.ReadAll(resp.Body)
		// using a separate variable cerr avoids overwriting the main err from ReadAll.
		if cerr := resp.Body.Close(); cerr != nil {
			writeLog("WARNING", fmt.Sprintf("failed to close response body: %v", cerr))
		}
		if err != nil {
			writeLog("WARNING", fmt.Sprintf("Failed to read response for Epic %s: %v", epicKey, err))
			epicTitles[epicKey] = "Epic Summary Lookup Failed"
			continue
		}

		// Check HTTP status
		if resp.StatusCode != 200 {
			writeLog("WARNING", fmt.Sprintf("HTTP %d error looking up Epic %s", resp.StatusCode, epicKey))
			epicTitles[epicKey] = "Epic Summary Lookup Failed"
			continue
		}

		// Parse JSON response
		var epicInfo EpicInfo
		if err := json.Unmarshal(body, &epicInfo); err != nil {
			writeLog("WARNING", fmt.Sprintf("Failed to parse Epic response for %s: %v", epicKey, err))
			epicTitles[epicKey] = "Epic Summary Lookup Failed"
			continue
		}

		// Extract epic title from summary only
		var epicTitle string
		if epicInfo.Fields.Summary != "" {
			epicTitle = epicInfo.Fields.Summary
		} else {
			epicTitle = "No Epic Title"
		}
		epicTitles[epicKey] = epicTitle
	}

	writeLog("INFO", fmt.Sprintf("Retrieved %d Epic summaries", len(epicTitles)))
	return epicTitles, nil
}

/***********************************************************************************************************************************/
// formatDate formats a date pointer to string in yyyy-MM-dd format
//
// Parameters:
//   datePtr - pointer to date string from Jira API
//
// Returns:
//   string - formatted date or empty string if null/invalid
func formatDate(datePtr *string) string {
	if datePtr == nil || *datePtr == "" {
		return ""
	}

	// Try to parse the Jira date format and convert to yyyy-MM-dd
	if parsedTime, err := time.Parse(time.RFC3339, *datePtr); err == nil {
		return parsedTime.Format("2006-01-02")
	}

	// If parsing fails, try other common formats
	formats := []string{
		"2006-01-02T15:04:05.000-0700",
		"2006-01-02T15:04:05.000Z",
		"2006-01-02T15:04:05-0700",
		"2006-01-02T15:04:05Z",
		"2006-01-02",
	}

	for _, format := range formats {
		if parsedTime, err := time.Parse(format, *datePtr); err == nil {
			return parsedTime.Format("2006-01-02")
		}
	}

	writeLog("WARNING", fmt.Sprintf("Error formatting date '%s'", *datePtr))
	return ""
}

/***********************************************************************************************************************************/
// extractFieldValues safely extracts various field values from issue with defaults
//
// Parameters:
//   issue - the Jira issue to extract values from
//
// Returns:
//   map[string]string - map of field names to values with appropriate defaults
func extractFieldValues(issue Issue) map[string]string {
	values := make(map[string]string)

	// Basic fields
	values["IssueType"] = issue.Fields.IssueType.Name
	values["Status"] = issue.Fields.Status.Name
	values["ProjectName"] = issue.Fields.Project.Name
	values["UpdatedDate"] = formatDate(issue.Fields.Updated)
	values["CreatedDate"] = formatDate(issue.Fields.Created)
	values["ResolvedDate"] = formatDate(issue.Fields.ResolutionDate)

	// Assignee
	if issue.Fields.Assignee != nil {
		values["Assignee"] = issue.Fields.Assignee.DisplayName
	} else {
		values["Assignee"] = "Unassigned"
	}

	// Creator/Reporter
	if issue.Fields.Creator != nil {
		values["Reporter"] = issue.Fields.Creator.DisplayName
	} else {
		values["Reporter"] = "Unknown"
	}

	// Story Points
	if issue.Fields.StoryPoints != nil {
		values["StoryPoints"] = fmt.Sprintf("%v", issue.Fields.StoryPoints)
	} else {
		values["StoryPoints"] = "N/A"
	}

	// Fix Versions
	var fixVersions []string
	for _, version := range issue.Fields.FixVersions {
		fixVersions = append(fixVersions, version.Name)
	}
	values["FixVersions"] = strings.Join(fixVersions, ", ")

	// Components
	var components []string
	for _, component := range issue.Fields.Components {
		components = append(components, component.Name)
	}
	values["Components"] = strings.Join(components, ", ")

	// Labels
	values["Labels"] = strings.Join(issue.Fields.Labels, ", ")

	// Resolution
	if issue.Fields.Resolution != nil {
		values["Resolution"] = issue.Fields.Resolution.Name
	} else {
		values["Resolution"] = ""
	}

	// Pair information
	if pairFieldProvided && pairFieldName != "" {
		// DEBUG: Show keys in AdditionalFields if debug is enabled
		if enableDebug {
			var keys []string
			for k := range issue.Fields.AdditionalFields {
				keys = append(keys, k)
			}
			writeLog("DEBUG", fmt.Sprintf("Issue %s AdditionalFields keys: %v", issue.Key, keys))
		}
		// Attempt to read the configured custom field from AdditionalFields
		if raw, ok := issue.Fields.AdditionalFields[pairFieldName]; ok && raw != nil {
			if enableDebug {
				writeLog("DEBUG", fmt.Sprintf("Issue %s raw Pair field (%s): %s", issue.Key, pairFieldName, string(raw)))
			}
			// Try several possible shapes: array of objects, single object, array of strings, or single string
			// 1) array of objects [{"displayName": "Alice"}, ...]
			var pairNames []string
			var parsed bool

			// attempt array of objects
			var pairMembers []PairMember
			if err := json.Unmarshal(raw, &pairMembers); err == nil && len(pairMembers) > 0 {
				for _, pair := range pairMembers {
					pairNames = append(pairNames, pair.DisplayName)
				}
				parsed = true
			}

			if !parsed {
				// attempt single object {"displayName":"Alice"}
				var single PairMember
				if err := json.Unmarshal(raw, &single); err == nil && single.DisplayName != "" {
					pairNames = append(pairNames, single.DisplayName)
					parsed = true
				}
			}

			if !parsed {
				// attempt array of strings ["Alice","Bob"]
				var strArr []string
				if err := json.Unmarshal(raw, &strArr); err == nil && len(strArr) > 0 {
					pairNames = append(pairNames, strArr...)
					parsed = true
				}
			}

			if !parsed {
				// attempt single string "Alice"
				var s string
				if err := json.Unmarshal(raw, &s); err == nil && s != "" {
					pairNames = append(pairNames, s)
					parsed = true
				}
			}

			if parsed {
				values["Pair"] = strings.Join(pairNames, ", ")
			} else {
				values["Pair"] = ""
			}
		} else {
			// Field not present in this issue
			values["Pair"] = ""
		}
	} else {
		// No custom field configured; use literal header/placeholder
		values["Pair"] = "Pair"
	}
	return values
}

/***********************************************************************************************************************************/
// writeOutputFile writes the spillover issues to a tab-separated file
//
// Parameters:
//   filename        - output filename
//   multisprintIssues - slice of issues that span multiple sprints
//   epicTitles      - map of epic keys to titles
//   appendMode      - if true, append to existing file; if false, create new file
//
// Returns:
//   error - any error encountered during file writing
func writeOutputFile(filename string, multisprintIssues []MultisprintIssue, epicTitles map[string]string, appendMode bool) (int, error) {
	// Ensure filename has .tsv extension
	if !strings.HasSuffix(filename, ".tsv") {
		filename += ".tsv"
	}

	var file *os.File
	var err error
	var writeHeader bool

	if appendMode {
		// Check if file exists to determine if we need to write header
		if _, statErr := os.Stat(filename); os.IsNotExist(statErr) {
			writeHeader = true
		}

		// Open file in append mode
		file, err = os.OpenFile(filename, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			return 0, fmt.Errorf("failed to open output file for append: %w", err)
		}
		writeLog("INFO", fmt.Sprintf("Appending to existing file: %s", filename))
	} else {
		// Create new file (overwrites existing)
		file, err = os.Create(filename)
		if err != nil {
			return 0, fmt.Errorf("failed to create output file: %w", err)
		}
		writeHeader = true
		writeLog("INFO", fmt.Sprintf("Creating new file: %s", filename))
	}
	defer func() {
		if err := file.Close(); err != nil {
			log.Printf("failed to close file: %v", err)
		}
	}()

	// Write header row only if needed (new file or append to empty file)
	if writeHeader {
		header := []string{
			"Issue Type",
			"Issue Key",
			"Summary",
			"Status",
			"Updated Date",
			"Created Date",
			"Resolved Date",
			"Assignee",
			"Pair",
			"Project",
			"Fix Versions",
			"Components",
			"Story Points",
			"Epic Link",
			"Epic Summary",
			"Labels",
			"Resolution",
			"Reporter",
			"Number of Sprints",
			"First Sprint",
			"Last Sprint",
			"All Sprints",
		}

		// Write header
		if _, err := file.WriteString(strings.Join(header, "\t") + "\n"); err != nil {
			return 0, fmt.Errorf("failed to write header: %w", err)
		}
	}

	// Write data rows
	pairFieldFoundCount := 0
	for _, multisprintIssue := range multisprintIssues {
		issue := multisprintIssue.Issue
		values := extractFieldValues(issue)
		// Debug: log the Pair value for each issue
		if enableDebug && pairFieldProvided && pairFieldName != "" {
			writeLog("DEBUG", fmt.Sprintf("writeOutputFile: Issue %s Pair value: '%s'", issue.Key, values["Pair"]))
		}
		// Count non-empty Pair field for found count (trim whitespace)
		if pairFieldProvided && pairFieldName != "" && strings.TrimSpace(values["Pair"]) != "" {
			pairFieldFoundCount++
		}
		// Get epic summary
		epicTitle := epicTitles[multisprintIssue.EpicLink]
		if epicTitle == "" {
			epicTitle = "No Epic Summary"
		}
		// Build row data
		row := []string{
			values["IssueType"],
			issue.Key,
			issue.Fields.Summary,
			values["Status"],
			values["UpdatedDate"],
			values["CreatedDate"],
			values["ResolvedDate"],
			values["Assignee"],
			values["Pair"],
			values["ProjectName"],
			values["FixVersions"],
			values["Components"],
			values["StoryPoints"],
			multisprintIssue.EpicLink,
			epicTitle,
			values["Labels"],
			values["Resolution"],
			values["Reporter"],
			fmt.Sprintf("%d", multisprintIssue.SprintInfo.SprintCount),
			multisprintIssue.SprintInfo.FirstSprint,
			multisprintIssue.SprintInfo.LastSprint,
			multisprintIssue.SprintInfo.AllSprints,
		}
		// Write row
		if _, err := file.WriteString(strings.Join(row, "\t") + "\n"); err != nil {
			return 0, fmt.Errorf("failed to write data row: %w", err)
		}
	}

	if appendMode {
		writeLog("INFO", fmt.Sprintf("Successfully appended %d issues to %s", len(multisprintIssues), filename))
	} else {
		writeLog("INFO", fmt.Sprintf("Successfully wrote %d issues to %s", len(multisprintIssues), filename))
	}
	return pairFieldFoundCount, nil
}

/***********************************************************************************************************************************/
// getLoggingFlagFromCommandLine checks for -log parameter in command line arguments
//
// This function scans command line arguments for a -log parameter and returns
// true if found, enabling log file creation.
//
// Parameters: None (reads from os.Args)
//
// Returns:
//   bool - true if -log flag is present, false otherwise
//
// Side effects:
//   - Prints status message if logging flag is found
func getLoggingFlagFromCommandLine() bool {
	args := os.Args[1:]
	for _, arg := range args {
		if strings.ToLower(arg) == "-log" {
			fmt.Println("Logging enabled from command line")
			return true
		}
	}
	return false
}

// getPairFromCommandLine checks for -Pair <fieldname> parameter in command line arguments
// If provided, it returns the field name and sets pairFieldProvided to true
func getPairFromCommandLine() string {
	args := os.Args[1:]
	for i, arg := range args {
		if strings.ToLower(arg) == "-pair" && i+1 < len(args) {
			// record provided flag
			pairFieldProvided = true
			// User-supplied field name should be used as-is (case-sensitive in Jira JSON)
			return args[i+1]
		}
	}
	pairFieldProvided = false
	return ""
}

/***********************************************************************************************************************************/
// cleanup performs cleanup operations before program exit
//
// This function ensures proper cleanup of resources, particularly the log file,
// and calculates execution time statistics.
//
// Parameters: None
// Returns: None
// Side effects: Closes log file, displays execution time to console
func cleanup() {
	// Calculate and display execution time
	duration := time.Since(startTime)
	fmt.Printf("\nExecution completed in %.2f seconds\n", duration.Seconds())

	// Log execution time if logging is enabled
	if enableLogging && logFile != nil {
		writeLog("INFO", fmt.Sprintf("Script execution completed in %.2f seconds", duration.Seconds()))
		// Close log file
		if err := logFile.Close(); err != nil {
			log.Printf("failed to close log file: %v", err)
		}
	}
}

/***********************************************************************************************************************************/
// showUsage displays program usage information and help text
//
// This function provides comprehensive help information including:
// 1. Program name and version information
// 2. Author attribution
// 3. Command line usage syntax
// 4. Detailed parameter descriptions
// 5. Practical usage examples
//
// Called when user specifies -?, /?, --help, or -help flags
//
// Parameters: None
// Returns: None
// Side effects: Prints formatted help text to stdout
func showUsage() {
	fmt.Printf(`
%s v%s

Identifies and reports on Jira "spillover" issues - work items that weren't completed within their originally planned sprint. This tool helps teams track delivery efficiency and improve planning.

Usage:
  %s.exe [-TokenFile token_file_path] [-url jira_base_url] [-project project_key] [-fromdate yyyy-mm-dd] [-daysprior #] [-outputfile filename] [-append] [-log] [-?]

Parameters:
  -TokenFile    Path to file containing Jira API token (username:api-token format)
  -url          Jira base URL (e.g., https://jira.company.com)
  -project      Jira project key (e.g., EXPD)
  -pair         Optional custom field name to use for Pair data (e.g., customfield_22311)
  -fromdate     Optional start date in yyyy-mm-dd format. Overrides daysprior if supplied
  -daysprior    Optional number of days prior to today to check (default: %d)
  -outputfile   Optional name for output file (default: spillover_rpt.tsv)
  -append       Append to existing output file instead of overwriting
  -log          Enable logging to file
  -debug		Enable display of each work item's sprint data during processing
  -?            Show this help message

Examples:
  %s.exe -project EXPD -daysprior 14 -outputfile spillover_report.tsv -log
  %s.exe -TokenFile c:\tokens\jira.tsv -url https://jira.company.com -project TEAM -fromdate 2025-01-01
  %s.exe -project EXPD -daysprior 7 -outputfile weekly_report.tsv -append

With no command line parameters, you will be prompted interactively for required values.

Authentication:
  Token file must contain a single line in format: username:api-token
  Create API tokens from your Jira profile settings.

Output:
  Tab-separated text file containing issues that have been worked on in multiple sprints.
  File includes issue details, sprint information, epic data, and assignment information.

`, programName, programVersion, programName, defaultDaysPrior, programName, programName, programName)
}

/***********************************************************************************************************************************/
// main is the entry point of the application
//
// The function handles both interactive prompts and command-line argument processing,
// providing flexibility for both manual use and automated scripting scenarios.
//
// Parameters: None (uses command line arguments via os.Args)
// Returns: None (exits with status 0 on success, 1 on error)
func main() {
	// Register cleanup function to ensure proper resource cleanup
	defer cleanup()

	// Check for help flags first
	args := os.Args[1:]
	for _, arg := range args {
		if arg == "-?" || arg == "/?" || arg == "--help" || arg == "-help" {
			showUsage()
			return
		}
	}

	// Check if logging should be enabled
	enableLogging = getLoggingFlagFromCommandLine()

	// Check if debug output should be enabled
	enableDebug = getDebugFlagFromCommandLine()

	// Initialize logging system
	if err := initLogging(); err != nil {
		fmt.Printf("Error initializing logging: %v\n", err)
		os.Exit(1)
	}

	// Display program banner
	fmt.Printf("\n\033[36m%s v%s\033[0m\n", programName, programVersion)
	writeLog("INFO", fmt.Sprintf("Starting %s v%s", programName, programVersion))

	// Get Jira base URL
	jiraBaseURL := getJiraBaseURL()

	// Get authentication token
	authToken, err := getAuthToken()
	if err != nil {
		writeLog("ERROR", fmt.Sprintf("Failed to get authentication token: %v", err))
		os.Exit(1)
	}

	// Get project key
	projectKey := getProjectFromCommandLine()
	if projectKey == "" {
		projectKey, err = getProjectKeyInteractively()
		if err != nil {
			writeLog("ERROR", fmt.Sprintf("Failed to get project key: %v", err))
			os.Exit(1)
		}
	}

	// Validate project key format (uppercase letters and numbers only)
	if !regexp.MustCompile(`^[A-Z0-9]+$`).MatchString(projectKey) {
		writeLog("ERROR", fmt.Sprintf("Project key '%s' must consist only of uppercase letters and numbers", projectKey))
		os.Exit(1)
	}

	// Get date range parameters
	fromDate, daysPrior, fromDateProvided, daysPriorProvided := getDateAndDaysFromCommandLine()

	// If neither parameter was provided via command line, prompt interactively
	if !fromDateProvided && !daysPriorProvided {
		fromDate, daysPrior, err = getDateRangeInteractively()
		if err != nil {
			writeLog("ERROR", fmt.Sprintf("Failed to get date range: %v", err))
			os.Exit(1)
		}
	}

	// Validate from date if provided
	if fromDate != "" {
		if err := validateDate(fromDate, "from date"); err != nil {
			writeLog("ERROR", err.Error())
			os.Exit(1)
		}

		// Calculate days prior from the provided date
		if parsedDate, err := time.Parse("2006-01-02", fromDate); err == nil {
			daysPrior = int(time.Since(parsedDate).Hours() / 24)
			writeLog("INFO", fmt.Sprintf("Using date range: %s to present (%d days)", fromDate, daysPrior))
		} else {
			writeLog("ERROR", fmt.Sprintf("Failed to parse from date: %v", err))
			os.Exit(1)
		}
	} else {
		// Use days prior
		if daysPrior <= 0 {
			daysPrior = defaultDaysPrior
		}
		fromDateTime := time.Now().AddDate(0, 0, -daysPrior)
		writeLog("INFO", fmt.Sprintf("Using date range: %s to present (%d days)",
			fromDateTime.Format("2006-01-02"), daysPrior))
	}

	// Get output filename
	outputFile := getOutputFileFromCommandLine()
	if outputFile == "" {
		outputFile, err = getOutputFileInteractively()
		if err != nil {
			writeLog("ERROR", fmt.Sprintf("Failed to get output filename: %v", err))
			os.Exit(1)
		}
	}

	// Get append flag
	appendMode := getAppendFlagFromCommandLine()

	// Get Pair field from command line (optional)
	pairFieldName = getPairFromCommandLine()

	// Validate project exists
	if err := validateProject(jiraBaseURL, authToken, projectKey); err != nil {
		writeLog("ERROR", fmt.Sprintf("Project validation failed: %v", err))
		fmt.Printf("\nProject '%s' not found in Jira. Please verify the project key is correct.\n", projectKey)
		os.Exit(1)
	}

	// Build JQL query
	jqlQuery := buildJQLQuery(projectKey, daysPrior)

	// Define required fields for API request
	// Build list of fields to request from Jira. Only include the custom Pair field if the user supplied -Pair.
	requiredFields := []string{
		"issuetype", "key", "summary", "status", "updated", "created", "resolutiondate",
		"assignee",
		// the pair field (if configured) will be inserted after "assignee"
		"fixVersions", "components", defaultStoryPointsField,
		defaultEpicLinkField, "labels", "resolution", defaultSprintField, "creator", "project",
	}
	if pairFieldProvided && pairFieldName != "" {
		// insert the user-specified field name after "assignee"
		// find index of "assignee"
		idx := -1
		for i, v := range requiredFields {
			if v == "assignee" {
				idx = i
				break
			}
		}
		if idx >= 0 {
			// insert at idx+1
			before := append([]string{}, requiredFields[:idx+1]...)
			after := append([]string{}, requiredFields[idx+1:]...)
			requiredFields = append(append(before, pairFieldName), after...)
		} else {
			// fallback: append to the front
			requiredFields = append([]string{pairFieldName}, requiredFields...)
		}
	}
	fieldsParam := strings.Join(requiredFields, ",")

	// Fetch all issues
	writeLog("INFO", "Fetching issues from Jira...")
	issues, err := fetchAllJiraIssues(jiraBaseURL, authToken, jqlQuery, fieldsParam)
	if err != nil {
		writeLog("ERROR", fmt.Sprintf("Failed to fetch issues: %v", err))
		os.Exit(1)
	}

	if len(issues) == 0 {
		writeLog("WARNING", "No issues found matching the criteria")
		return
	}

	writeLog("INFO", fmt.Sprintf("Processing %d issues to identify multi-sprint items...", len(issues)))

	// Process issues to find spillovers
	var multisprintIssues []MultisprintIssue
	var epicKeysToLookup []string
	epicKeySet := make(map[string]bool) // To avoid duplicates

	for i, issue := range issues {
		if i%100 == 0 {
			writeLog("INFO", fmt.Sprintf("Processing issue %d of %d: %s", i+1, len(issues), issue.Key))
		}

		// Skip issues resolved too long ago if they have resolution date
		if issue.Fields.ResolutionDate != nil {
			if resolvedTime, err := time.Parse(time.RFC3339, *issue.Fields.ResolutionDate); err == nil {
				daysSinceResolved := int(time.Since(resolvedTime).Hours() / 24)
				if daysSinceResolved > daysPrior {
					continue
				}
			}
		}

		// DEBUG: Log the raw SprintField value for this issue if debug flag is set
		if enableDebug {
			var debugLines []string
			debugLines = append(debugLines, fmt.Sprintf("Issue %s raw SprintField:", issue.Key))
			switch sprints := issue.Fields.SprintField.(type) {
			case []interface{}:
				for _, sprint := range sprints {
					if sprintMap, ok := sprint.(map[string]interface{}); ok {
						// Get id and name for header
						idVal := sprintMap["id"]
						nameVal := sprintMap["name"]
						idStr := fmt.Sprintf("%v", idVal)
						nameStr := fmt.Sprintf("%v", nameVal)
						debugLines = append(debugLines, fmt.Sprintf("Sprint %s: %s", idStr, nameStr))
						// Board ID
						if val := sprintMap["boardId"]; val != nil {
							debugLines = append(debugLines, fmt.Sprintf("  - Board ID: %v", val))
						}
						// State
						if val, ok := sprintMap["state"]; ok {
							debugLines = append(debugLines, fmt.Sprintf("  - State: %v", val))
						}
						// Goal
						if val, ok := sprintMap["goal"]; ok {
							goalStr := fmt.Sprintf("%v", val)
							if goalStr == "" {
								goalStr = "(empty)"
							}
							debugLines = append(debugLines, fmt.Sprintf("  - Goal: %s", goalStr))
						} else {
							debugLines = append(debugLines, "  - Goal: (empty)")
						}
						// Start Date
						if val, ok := sprintMap["startDate"]; ok {
							debugLines = append(debugLines, fmt.Sprintf("  - Start Date: %v", val))
						}
						// End Date
						if val, ok := sprintMap["endDate"]; ok {
							debugLines = append(debugLines, fmt.Sprintf("  - End Date:   %v", val))
						}
						// Complete Date
						if val, ok := sprintMap["completeDate"]; ok {
							debugLines = append(debugLines, fmt.Sprintf("  - Complete Date: %v", val))
						}
					}
				}
			case nil:
				debugLines = append(debugLines, "  [No sprint data]")
			default:
				debugLines = append(debugLines, fmt.Sprintf("  [Unrecognized SprintField type: %T]", sprints))
			}
			writeLog("DEBUG", strings.Join(debugLines, "\n"))
		}

		// Parse sprint information
		sprintInfo := parseSprintField(issue.Fields.SprintField)

		// Only include issues that have been in more than one sprint
		if sprintInfo.SprintCount > 1 {
			epicLink := getEpicLink(issue.Fields.EpicLinkField)

			// Add to multi-sprint issues
			multisprintIssue := MultisprintIssue{
				Issue:         issue,
				WorkedSprints: sprintInfo.SprintCount,
				EpicLink:      epicLink,
				SprintInfo:    sprintInfo,
			}

			// Set resolved date if available
			if issue.Fields.ResolutionDate != nil {
				if resolvedTime, err := time.Parse(time.RFC3339, *issue.Fields.ResolutionDate); err == nil {
					multisprintIssue.ResolvedDate = &resolvedTime
				}
			}

			multisprintIssues = append(multisprintIssues, multisprintIssue)

			// Collect epic keys for lookup
			if epicLink != "No Epic" && !epicKeySet[epicLink] {
				epicKeySet[epicLink] = true
				epicKeysToLookup = append(epicKeysToLookup, epicLink)
			}
		}
	}

	writeLog("INFO", fmt.Sprintf("Found %d issues that have been worked on in multiple sprints", len(multisprintIssues)))

	// Debug: Show how many issues had a non-empty Pair field
	// (moved to after writeOutputFile call, using local variable)

	// Fetch epic summaries
	var epicTitles map[string]string
	if len(epicKeysToLookup) > 0 {
		epicTitles, err = fetchEpicTitles(jiraBaseURL, authToken, epicKeysToLookup)
		if err != nil {
			writeLog("WARNING", fmt.Sprintf("Failed to fetch some epic summaries: %v", err))
			// Continue with empty epic summary map
			epicTitles = make(map[string]string)
		}
	} else {
		epicTitles = make(map[string]string)
	}

	// Write output file
	writeLog("INFO", "Formatting output data...")
	pairFieldFoundCount, err := writeOutputFile(outputFile, multisprintIssues, epicTitles, appendMode)
	if err != nil {
		writeLog("ERROR", fmt.Sprintf("Failed to write output file: %v", err))
		os.Exit(1)
	}
	// Debug: Show how many issues had a non-empty Pair field
	if enableDebug && pairFieldProvided && pairFieldName != "" {
		writeLog("DEBUG", fmt.Sprintf("pairFieldFoundCount after processing: %d", pairFieldFoundCount))
	}
	// If user supplied -Pair but the field wasn't found on any issue, warn the user
	if pairFieldProvided && pairFieldName != "" && pairFieldFoundCount == 0 {
		writeLog("WARNING", fmt.Sprintf("Pair field '%s' was requested but not found on any issues. Check the field name.", pairFieldName))
		fmt.Printf("Warning: Pair field '%s' was requested but not found on any issues. Check the field name.\n", pairFieldName)
	}

	fmt.Printf("\n\033[32mSuccess!\033[0m Processed %d issues and found %d spillover issues.\n",
		len(issues), len(multisprintIssues))
	if appendMode {
		fmt.Printf("Results appended to: %s\n", outputFile)
	} else {
		fmt.Printf("Results saved to: %s\n", outputFile)
	}
}
