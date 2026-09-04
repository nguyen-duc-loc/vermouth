// Command platformconfig validates and reads the machine readable local platform contract.
//
//nolint:err113,mnd,noinlineerr // CLI usage errors carry the exact rejected command and argument count.
package main

import (
	"errors"
	"fmt"
	"os"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "platformconfig: %v\n", err)
		os.Exit(1)
	}
}

//nolint:funlen,gocognit // The command switch keeps every CLI usage contract visible in one place.
func run(args []string) error {
	if len(args) == 0 {
		return errors.New(
			"expected validate-inputs, get, image-plan, production-image-plan, production-images-document, production-migration-set-sha256, production-export-manifest, production-archive-validate, tree-sha256, validate-document, or secret-hash",
		)
	}

	switch args[0] {
	case "validate-inputs":
		if len(args) != 2 {
			return errors.New("usage: platformconfig validate-inputs <repository-root>")
		}
		return validateInputs(args[1])
	case "get":
		if len(args) != 3 {
			return errors.New("usage: platformconfig get <yaml-file> <path>")
		}
		return getValue(args[1], args[2], os.Stdout)
	case "image-plan":
		if len(args) != 4 {
			return errors.New("usage: platformconfig image-plan <repository-root> <workload> <native|multi>")
		}
		return writeImagePlan(args[1], args[2], args[3], os.Stdout)
	case "production-image-plan":
		if len(args) != 5 {
			return errors.New(
				"usage: platformconfig production-image-plan <repository-root> <workload> <source-revision> <dockerhub-namespace>",
			)
		}
		return writeProductionImagePlan(args[1], args[2], args[3], args[4], os.Stdout)
	case "production-images-document":
		if len(args) != 3 {
			return errors.New(
				"usage: platformconfig production-images-document <metadata-json> <verified-records-jsonl>",
			)
		}
		return writeProductionImagesDocument(args[1], args[2], os.Stdout)
	case "production-migration-set-sha256":
		if len(args) != 2 {
			return errors.New("usage: platformconfig production-migration-set-sha256 <repository-root>")
		}
		return writeProductionMigrationSetSHA256(args[1], os.Stdout)
	case "production-export-manifest":
		if len(args) != 3 {
			return errors.New("usage: platformconfig production-export-manifest <metadata-json> <tree-root>")
		}
		return writeProductionExportManifest(args[1], args[2], os.Stdout)
	case "production-archive-validate":
		if len(args) != 1 {
			return errors.New("usage: platformconfig production-archive-validate")
		}
		return validateProductionArchive(os.Stdin, os.Stdout)
	case "tree-sha256":
		if len(args) != 2 {
			return errors.New("usage: platformconfig tree-sha256 <directory>")
		}
		return writeTreeSHA256(args[1], os.Stdout)
	case "validate-document":
		if len(args) != 3 {
			return errors.New("usage: platformconfig validate-document <schema-file> <document>")
		}
		return validateDocument(args[1], args[2])
	case "secret-hash":
		if len(args) != 1 {
			return errors.New("usage: platformconfig secret-hash")
		}
		return writeSecretHash(os.Stdin, os.Stdout)
	case "atomic-commit":
		if len(args) != 3 {
			return errors.New("usage: platformconfig atomic-commit <next-file> <target-file>")
		}
		return atomicCommit(args[1], args[2])
	case "validate-rendered":
		paths, err := parseRenderedPaths(args)
		if err != nil {
			return err
		}
		return validateRendered(paths)
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func parseRenderedPaths(args []string) (renderedPaths, error) {
	if len(args) != 6 && len(args) != 7 {
		return renderedPaths{}, errors.New(
			"usage: platformconfig validate-rendered <traffic-matrix> <workload-matrix> <foundation> <application> <jobs> [google-enabled]",
		)
	}
	googleEnabled := false
	if len(args) == 7 {
		if args[6] != environmentGoogleEnabled {
			return renderedPaths{}, fmt.Errorf("unknown rendered environment %q", args[6])
		}
		googleEnabled = true
	}
	return renderedPaths{
		traffic:       args[1],
		workloads:     args[2],
		foundation:    args[3],
		applications:  args[4],
		jobs:          args[5],
		googleEnabled: googleEnabled,
	}, nil
}
