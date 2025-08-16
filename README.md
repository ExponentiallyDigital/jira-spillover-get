# jira-spillover-get

A tool to identify and report on Jira "spillover": work items that weren't completed within their originally planned sprint. This tools helps teams track delivery efficiency and improve planning.

### Table of contents
<!-- vscode-markdown-toc -->
* [Why spillover matters](#Whyspillovermatters)
* [What does *jira-spillover-get* do?](#Whatdoesjira-spillover-getdo)
* [Key features](#Keyfeatures)
  * [Who should use this tool](#Whoshouldusethistool)
  * [Use cases](#Usecases)
  * [Project structure](#Projectstructure)
* [Prerequisites](#Prerequisites)
* [Set up](#Setup)
  * [PowerShell](#PowerShell)
  * [Windows command line](#Windowscommandline)
  * [Linux/WSL](#LinuxWSL)
* [Build the application](#Buildtheapplication)
  * [Windows](#Windows)
  * [Linux/macOS](#LinuxmacOS)
  * [Building from source](#Buildingfromsource)
  * [Testing](#Testing)
* [Usage](#Usage)
  * [Basic execution](#Basicexecution)
  * [Command line options](#Commandlineoptions)
  * [Command line parameters](#Commandlineparameters)
  * [Parameters](#Parameters)
  * [Examples](#Examples)
  * [Append mode feature](#Appendmodefeature)
  * [Automated execution](#Automatedexecution)
* [Output format](#Outputformat)
* [Interpreting results](#Interpretingresults)
  * [Key metrics to review](#Keymetricstoreview)
  * [Common patterns and what they mean](#Commonpatternsandwhattheymean)
  * [Recommended actions](#Recommendedactions)
* [How jira-spillover-get works](#Howjira-spillover-getworks)
  * [JQL query](#JQLquery)
  * [Jira field mappings](#Jirafieldmappings)
* [Error handling](#Errorhandling)
* [Logging](#Logging)
* [Performance considerations](#Performanceconsiderations)
* [Troubleshooting](#Troubleshooting)
  * [Common issues](#Commonissues)
  * [Debug information](#Debuginformation)
* [Contributing](#Contributing)
* [License](#License)
* [Support](#Support)

<!-- vscode-markdown-toc-config
	numbering=false
	autoSave=true
	/vscode-markdown-toc-config -->
<!-- /vscode-markdown-toc -->

## <a name='Whyspillovermatters'></a>Why spillover matters

Spillover represents an inability to complete what was planned within a sprint. While occasional spillover can be justified, ongoing spillover can significantly impact Agile delivery. It indicates issues with planning and execution, leading to delays and reduced efficiency.

The biggest challenge with spillover is tracking its occurrence, as Jira doesn't provide built-in tools specifically for this purpose. This tool fills that gap by creating a TSV (tab separated variabe) output file of work items which have spilled over. The TSV file can be manipulated by Excel of simiar tools to produce rich graphical reporting.

For an in-depth examination of why spillover matters, how to analyse it, and most importantly, what strategic approaches you can implement to reduce it, check out my <exponentiallydigital.com> blog post "[Mastering agile spillover, analysis and strategic solutions](https://www.exponentiallydigital.com/mastering-agile-spillover-analysis-and-strategic-solutions/)".

## <a name='Whatdoesjira-spillover-getdo'></a>What does *jira-spillover-get* do?

*jira-spillover-get* queries a specified Jira project via Atlassian APIs to identify all issues (excluding epics, risks, and sub-tasks) that:

1. were modified within a user-defined timeframe
2. spanned multiple sprints, indicating spillover

Results are shown on-screen and exported as a tab-separated text file for analysis in Excel or similar tools. It can run interactively or in automated workflows such as scheduled tasks or CI/CD pipelines.

## <a name='Keyfeatures'></a>Key features

* **Multi-sprint Detection** identifies issues that have been worked on in more than one sprint
* **Flexible Date Filtering** filter by specific date range or days prior to today
* **Epic Title Lookup** automatically retrieves epic titles for linked issues
* **Command Line Interface** full command line support with interactive fallback
* **Append Mode** option to append results to existing files instead of overwriting (useful for pulling data from multiple projects)
* **Progress Tracking** real-time progress indicators for large datasets
* **Comprehensive Logging** optional detailed logging to timestamped files
* **Authentication** secure file-based API token authentication
* **Project Validation** validates project existence before processing
* **TSV Output** tab-separated values output for easy Excel import

### <a name='Whoshouldusethistool'></a>Who should use this tool

* Agile teams looking to reduce their spillover rate
* Scrum Masters and Agile Coaches tracking team efficiency
* Development Team Leads identifying planning improvement opportunities
* Project Managers monitoring delivery predictability

### <a name='Usecases'></a>Use cases

* Use with sprint reviews to help identify continuous improvement opportunities
* Combine multiple project spillover data for portfolio analysis
* Building long-term trend data for retrospectives
* Automated reporting pipelines that accumulate data over time

### <a name='Projectstructure'></a>Project structure

```text
└── jira-spillover-get/
    ├── .gitignore
    ├── LICENSE
    ├── README.md                       # The file you're reading now :D
    ├── build/
    │   ├── jira-spillover-get.exe      # Windows executable
    │   ├── jira-spillover-get-linux    # Linux executable
    │   └── jira-spillover-get-mac      # macOS executable
    ├── go/
    │   ├── build.sh                    # Unix/Linux build script
    │   ├── build.bat                   # Windows build script
    │   ├── go.mod                      # Go module definition
    │   ├── jira-spillover-get.go       # Go application source code
    │   ├── resource.syso               # Dynamically created by go generate
    │   └── versioninfo.json            # Windows resource definition (file version details)
    └── samples/
        └── all-projects.bat            # batch file for portfolio analysis
```

## <a name='Prerequisites'></a>Prerequisites

1. **Jira API token** You need a valid Jira API token file
2. **Network access** Connection to your Jira instance
3. **Project access** Sufficient permissions to view the target Jira project

## <a name='Setup'></a>Set up

1. Clone or download the source code
2. Navigate to the `go` subdirectory
3. Create a Jira API token, see [Manage API tokens for your atlassian account](https://support.atlassian.com/atlassian-account/docs/manage-api-tokens-for-your-atlassian-account/)
4. Save the generated token somewhere safe eg a password manager etc.
5. Create a text file in a secure location eg. "c:\users\my-name\api-tokens\Jira-API-token.txt"
6. Add a single line to this file with your credentials in the format: `"your-email-address:your-api-token"`
7. Secure the API token file using one of the following methods

### <a name='PowerShell'></a>PowerShell

```powershell
icacls Jira-API-token.txt /inheritance:r /grant:r "$($env:USERNAME):(F)"
```

### <a name='Windowscommandline'></a>Windows command line

```cmd
icacls Jira-API-token.txt /inheritance:r /grant:r "%USERNAME%:(F)"
```

### <a name='LinuxWSL'></a>Linux/WSL

```bash
chmod 400 Jira-API-token.txt && chown $(whoami) Jira-API-token.txt
```

## <a name='Buildtheapplication'></a>Build the application

Precompiled binaries are supplied or build your own via

### <a name='Windows'></a>Windows

```batch
build.bat
```

### <a name='LinuxmacOS'></a>Linux/macOS

```bash
chmod +x build.sh
./build.sh
```

### <a name='Buildingfromsource'></a>Building from source

```bash
# Build for current platform
go build -o jira-spillover-get

# Build for Windows (source not specified to enable resource embedding)
go build -o jira-spillover-get.exe

# Build for Linux
GOOS=linux GOARCH=amd64 go build -o jira-spillover-get-linux jira-spillover-get.go

# Build for macOS
GOOS=darwin GOARCH=amd64 go build -o jira-spillover-get-mac jira-spillover-chart-get.go
```

NB requires <https://github.com/josephspurrier/goversioninfo> for embedding Windows file properties in the executable.

### <a name='Testing'></a>Testing

```bash
# Run with test parameters
./jira-spillover-get -help

# Validate build
go build && ./jira-spillover-get -help
```

## <a name='Usage'></a>Usage

### <a name='Basicexecution'></a>Basic execution

Execute the application from the command line. It will prompt you for required parameters if not provided:

```shell
jira-spillover-get.exe
```

### <a name='Commandlineoptions'></a>Command line options

View available command-line parameters:

```shell
jira-spillover-get.exe -? | -help
```

### <a name='Commandlineparameters'></a>Command line parameters

```text
jira-spillover-get.exe [-TokenFile token_file_path] [-url jira_base_url] 
    [-project project_key] [-fromdate yyyy-mm-dd] [-daysprior #] 
    [-outputfile filename] [-append] [-log] [-debug] [-? | /? | --help | -help]
```

### <a name='Parameters'></a>Parameters

* `-TokenFile` path to file containing Jira API token (username:api-token format)
* `-url` Jira base URL (e.g., `https://jira.company.com`)
* `-project` Jira project key (e.g., EXPD)
* `-fromdate` optional start date in yyyy-mm-dd format. Overrides daysprior if supplied
* `-daysprior` optional number of days prior to today to check (default: 10)
* `-outputfile` optional name for output file (default: issues_output.txt)
* `-append` append to existing output file instead of overwriting
* `-pair customfield_22311` specify the field name for paired assignees
* `-log` enable logging to file
* `-debug` enable display of each work item's sprint data during processing
* `-? | /? | --help | -help` show help message

### <a name='Examples'></a>Examples

**Command line with all parameters:**

```batch
jira-spillover-get.exe -project EXPD -daysprior 14 -outputfile spillover_report -append -pair customfield_10186 -log -debug
```

**Interactive mode (no parameters):**

```batch
jira-spillover-get.exe
```

**With authentication and URL:**

```batch
jira-spillover-get.exe -TokenFile c:\tokens\jira.txt -url https://jira.company.com -project TEAM -fromdate 2025-01-01
```

**Append mode for accumulating data:**

```batch
jira-spillover-get.exe -project EXPD -daysprior 7 -outputfile weekly_report.txt -append
```

**Combining multiple projects in one file:**

```batch
rem Create initial file
jira-spillover-get.exe -project PROJ1 -outputfile combined_report.txt

rem Append additional projects
jira-spillover-get.exe -project PROJ2 -outputfile combined_report.txt -append
jira-spillover-get.exe -project PROJ3 -outputfile combined_report.txt -append
```

**Using a custom Pair field**

If your Jira instance uses a custom field for pair programming information, supply it with `-Pair`.

```batch
jira-spillover-get.exe -project EXPD -daysprior 14 -Pair customfield_22311 -outputfile spillover_with_pair.tsv
```

### <a name='Appendmodefeature'></a>Append mode feature

The `-append` flag enables accumulation of data across multiple runs:

**Benefits:**

* **Historical Tracking** build historical datasets by appending weekly/monthly reports
* **Multi-Project Analysis** combine data from multiple projects into a single file
* **Incremental Data Collection** add new data without losing existing results
* **Automated Workflows** perfect for scheduled tasks that need to accumulate data over time

**Header management:**

* Headers are automatically written only when creating new files or appending to empty files
* No duplicate headers when appending to existing files with data
* Maintains proper tab-separated format throughout

### <a name='Automatedexecution'></a>Automated execution

The command line parameters allow you to run this application from a scheduler like Windows Task Scheduler or via a batch file:

```batch
jira-spillover-get.exe -project EXPD -daysprior 30 -outputfile spillover_report.txt -log
```

**Scheduled append example for weekly accumulation:**

```batch
rem Weekly scheduled task to append new spillover data
jira-spillover-get.exe -project EXPD -daysprior 7 -outputfile monthly_spillover.txt -append -log
```

## <a name='Outputformat'></a>Output format

The application generates a tab-separated text file containing detailed information about each spillover issue including:

* Issue type, key, and summary
* Status and key dates (updated, created, resolved)
* Assignment information (assignee, pair)
* Project metadata (fix versions, story points, epic information)
* Sprint information (number of sprints, first/last sprint)

The application generates a tab-separated text file with the following columns:

* Issue Type
* Issue Key
* Summary
* Status
* Updated Date
* Created Date
* Resolved Date
* Assignee
* Pair
* Project
* Fix Version/s
* Component/s
* Story Points
* Epic Link
* Epic Title
* Labels
* Resolution
* Reporter
* Number of Sprints
* First Sprint
* Last Sprint
* All Sprints

## <a name='Interpretingresults'></a>Interpreting results

### <a name='Keymetricstoreview'></a>Key metrics to review

1. **Total spillover count** the overall number of issues that have spilled over
2. **Sprint count** how many sprints each issue has been through
3. **Time in progress** duration between first sprint and last sprint
4. **Issue types** distribution of spillover across stories, bugs, and tasks
5. **Epics** epics which have a high number of issues which spilled over

### <a name='Commonpatternsandwhattheymean'></a>Common patterns and what they mean

* **High Bug Spillover** may indicate quality issues or inadequate testing
* **Story Spillover** often suggests scope creep or inadequate refinement
* **Multiple Sprint Spillover** issues carried across more than two sprints may indicate blocked work
* **Epics** unclear, poorly defined or frequently changing requirements

### <a name='Recommendedactions'></a>Recommended actions

* For issues with high sprint counts, consider breaking them down into smaller pieces
* Review estimation practices if the same issue types consistently spill over
* Identify common blockers that lead to spillover and address them systematically
* Consider a team retrospective specifically focused on spillover patterns and solutions

## <a name='Howjira-spillover-getworks'></a>How jira-spillover-get works

### <a name='JQLquery'></a>JQL query

The application uses the following JQL pattern:

```text
project = {PROJECT} AND issuetype not in (Epic, Risk, 'Sub Task') 
    AND Sprint is not EMPTY AND updated >= -{DAYS}d
```

### <a name='Jirafieldmappings'></a>Jira field mappings

The application uses these Jira field mappings (configurable in source):

* **Story Points** `customfield_10002`
* **Sprint Field** `customfield_14181`
* **Epic Link** `customfield_14182`
* **Epic Title** `customfield_14183`
* **Pair Field** `customfield_22311`

## <a name='Errorhandling'></a>Error handling

The application includes comprehensive error handling for:

* Network connectivity issues
* Authentication failures
* Invalid project keys
* Malformed API responses
* File I/O errors
* Date format validation

## <a name='Logging'></a>Logging

When enabled with the `-log` flag, the application creates timestamped log files:

```text
jira-spillover-get-YYYYMMDD-HHMMSS.log
```

Logs include:

* API request details
* Progress information
* Error messages and warnings
* Execution timing statistics

## <a name='Performanceconsiderations'></a>Performance considerations

* Execution performance depends on the number of issues in your project and the time range selected, it has been optimized for batching Jira queries and parallel lookups
* For extremely large projects and a large date range, consider running after-hours or using smaller time ranges
* Typical execution time ranges from 2 seconds to minutes depending on how many days prior you elect and the volume of issues in your project
* Recommended execution frequency at the end of every sprint or for an entire program increment or set of increments
* This application can sucessfully return thousands of issues over multiple years

## <a name='Troubleshooting'></a>Troubleshooting

### <a name='Commonissues'></a>Common issues

1. **Token file not found**
   * Ensure the token file exists at the specified path
   * Check file permissions

2. **Authentication failed**
   * Verify your Jira credentials in the token file
   * Ensure the token file format is correct (`email-address:token`)
   * Check that the API token is still valid

3. **Project not found**
   * The project code might not exist
   * Check the project code spelling and case
   * Verify you have permission to view the project

4. **API connection issues**
   * Verify network connectivity to Jira
   * Check if you're behind a corporate firewall/proxy
   * Ensure the Jira URL is correct and accessible, try accessing it in a browser

5. **No issues found**
   * Check if the project has Story/Task/Bug issue types
   * Verify the date range (if specified) contains resolved issues
   * Ensure the project contains work items

6. **Export file issues**
   * Ensure you have write permissions to the target directory
   * Check that the output filename doesn't contain invalid characters
   * Verify the output directory exists (the tool creates files but not directories)
   * When using `-append`, ensure the existing file format matches the specified `-outfmt`

7. **Automatic date detection issues**
   * If no date range is detected, verify the project contains resolved issues
   * Check that issues have valid resolution dates in Jira
   * Ensure the project has completed work items (not just open issues)

### <a name='Debuginformation'></a>Debug information

The tool provides detailed progress information including:

* Authentication source and validation
* Project validation results
* JQL query construction
* API request progress and batch information
* Issue processing and with the optional '-debug' command line switch, display of each work item's sprint data during processing

All debug information is logged to timestamped log files for later analysis.

## <a name='Contributing'></a>Contributing

Contributions to improve this tool are welcome! To contribute:

1. Fork the repository
2. Create a feature branch
3. Make your changes
4. Submit a pull request with a clear description of the improvements

Please ensure your code follows existing style patterns and includes appropriate comments.

## <a name='License'></a>License

This program is free software: you can redistribute it and/or modify it under the terms of the GNU General Public License as published by the Free Software Foundation, either version 3 of the License, or (at your option) any later version.

This program is distributed in the hope that it will be useful, but WITHOUT ANY WARRANTY; without even the implied warranty of MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the GNU General Public License for more details.

You should have received a copy of the GNU General Public License along with this program. If not, see <https://www.gnu.org/licenses>.

For the purposes of attribution please use <https://www.linkedin.com/in/andrewnewbury>

Copyright (C) 2025 Andrew Newbury

## <a name='Support'></a>Support

This tool is unsupported and may cause objects in mirrors to be closer than they appear etc. Batteries not included.

It is strongly suggested that you test this tool in a non-production environment to ensure it meets your needs.
