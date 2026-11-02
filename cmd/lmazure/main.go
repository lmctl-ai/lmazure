// SPDX-License-Identifier: MIT

// Command lmazure serves the shared lmcloud schema for Azure. Everything
// provider-neutral comes from github.com/lmctl-ai/lmcloud/hostkit; this binary
// adds the Azure Blob/Queue adapter (internal/azure), lmazure's CLI surface,
// and its memory watchdog.
package main

import (
	"fmt"
	"io"
	"os"

	"github.com/lmctl-ai/lmazure/internal/azure"
	"github.com/lmctl-ai/lmazure/internal/membudget"
	"github.com/lmctl-ai/lmcloud/hostkit/cloudhost"
	"github.com/lmctl-ai/lmcloud/hostkit/invoke"
	"github.com/lmctl-ai/lmcloud/schema"
)

const Version = "0.1.5"

func main() { os.Exit(run(os.Args[1:], os.Stdout)) }

func run(args []string, out io.Writer) int {
	// Support explicit `--dry` flag in addition to LMCODE_INVOCATION_FILE.
	forceDry := false
	filtered := make([]string, 0, len(args))
	for _, a := range args {
		if a == "--dry" {
			forceDry = true
			continue
		}
		filtered = append(filtered, a)
	}
	args = filtered
	if forceDry && os.Getenv("LMCODE_INVOCATION_FILE") == "" {
		if f, err := os.CreateTemp("", "lmazure-dry-*.json"); err == nil {
			_, _ = f.WriteString(`{"dry":true}`)
			_ = f.Close()
			defer os.Remove(f.Name())
			_ = os.Setenv("LMCODE_INVOCATION_FILE", f.Name())
			defer os.Unsetenv("LMCODE_INVOCATION_FILE")
		}
	}

	prog := cloudhost.Program{
		Name:    "lmazure",
		Version: Version,
		Schema:  schema.Bytes,
		NewConfig: func(_ bool) (cloudhost.Config, error) {
			adapter := azure.New(nil)
			return cloudhost.Config{
				Provider:  "azure",
				Store:     adapter,
				Queue:     adapter,
				Endpoints: azure.Endpoints(),
			}, nil
		},
		Out: out,
		Hooks: invoke.Hooks{
			BeforeRun:     membudget.ResetRun,
			StartWatchdog: membudget.StartWatchdog,
		},
		Init: func() error {
			_, err := membudget.InitFromEnv()
			return err
		},
		Help:    helpText(),
		Notices: noticesText,
		Commands: map[string]func([]string, io.Writer) int{
			"--sweep": func(_ []string, w io.Writer) int {
				fmt.Fprintln(w, `{"ok":true,"swept":0,"mode":"sim-or-fake-only"}`)
				return 0
			},
		},
	}
	return prog.Main(args)
}

func helpText() string {
	return fmt.Sprintf(`lmazure %s — Go lmcode host for Azure (shared lmcloud schema, built on lmcloud hostkit)

Usage:
  lmazure [cloud] [--lmcode|--first|--last] FILE ...
  lmazure [cloud] --lmcode-schema [--json]
  lmazure [cloud] --lmcode-signatures [--without-std]
  lmazure --sweep [RUN_ID]
  lmazure --notices | --version | -V | --help | -h

Dry runs come from the caller's invocation manifest (lmscript --dry-run).

Endpoints (validated; plain http only for loopback; see lmcloud design 02 §3.3):
  set_endpoint(service|"all", url) > LMCLOUD_<SVC>_ENDPOINT > LMAZURE_<SVC>_ENDPOINT >
  LMCLOUD_ENDPOINT > LMAZURE_ENDPOINT >
  AZURE_STORAGE_BLOB_ENDPOINT / AZURE_STORAGE_QUEUE_ENDPOINT > AZURE_STORAGE_ENDPOINT / AZURE_ENDPOINT
  No endpoint: offline in-process fake. LMAZURE_LIVE=1 or a non-loopback endpoint:
  not supported yet (live DefaultAzureCredential support is a future phase).

Simulator credentials and scope (loopback endpoints only):
  LMAZURE_SIM_ACCOUNT             Storage account name (default: devstoreaccount1)
  LMAZURE_SIM_KEY                 Base64 SharedKey (default: Azurite/azuresim dev key)
  LMAZURE_SIM_TOKEN               Bearer token override
  LMAZURE_COMPAT_PROBE_EXISTENCE  Pre-operation existence probes for pre-7903c9c azuresim (default: 1)

Other:
  LMAZURE_MAX_MEMORY_MB           Memory ceiling in MiB (default 4096)
`, Version)
}

const noticesText = `lmazure Third-Party Software Notices
====================================

1. github.com/lmctl-ai/lmcode-go
   License: MIT
   Copyright (c) lmctl-ai

2. github.com/lmctl-ai/lmcloud (hostkit)
   License: MIT
   Copyright (c) lmctl-ai

3. github.com/Azure/azure-sdk-for-go/sdk/azcore
   License: MIT
   Copyright (c) Microsoft Corporation

4. github.com/Azure/azure-sdk-for-go/sdk/storage/azblob
   License: MIT
   Copyright (c) Microsoft Corporation

5. github.com/Azure/azure-sdk-for-go/sdk/storage/azqueue
   License: MIT
   Copyright (c) Microsoft Corporation
`
