package digest

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// finalPublicationPaths lists the only files a digest run may remove. They
// are both task-owned representations of the final article; checkpoints are
// deliberately excluded so a failed run remains resumable.
func finalPublicationPaths(opts Options) []string {
	paths := make([]string, 0, 2)
	if opts.OutPath != "" {
		paths = append(paths, opts.OutPath)
	}
	if opts.ArtifactDir != "" {
		artifact := filepath.Join(opts.ArtifactDir, "responses", "rewrite.md")
		if artifact != opts.OutPath {
			paths = append(paths, artifact)
		}
	}
	return paths
}

// clearFinalPublication removes stale task-owned final files before a run and
// after any failed run. It never removes directories or intermediate
// checkpoints, and a removal failure is surfaced rather than ignored.
func clearFinalPublication(opts Options) error {
	for _, path := range finalPublicationPaths(opts) {
		if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("digest: remove final publication %q: %w", path, err)
		}
	}
	return nil
}

// reservePublicationStage allocates an owned same-directory staging path, so
// each final rename is atomic and never crosses filesystems.
func reservePublicationStage(path string) (string, error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return "", fmt.Errorf("create publication staging directory: %w", err)
	}
	f, err := os.CreateTemp(dir, "."+filepath.Base(path)+".distill-stage-*")
	if err != nil {
		return "", fmt.Errorf("reserve publication stage: %w", err)
	}
	stage := f.Name()
	if err := f.Close(); err != nil {
		_ = os.Remove(stage)
		return "", fmt.Errorf("close publication stage: %w", err)
	}
	return stage, nil
}

// publishFinal stages both final representations before publishing either.
// If the artifact rename fails after --out is published, --out is rolled back
// so a failed run never leaves a final article visible at either path.
func publishFinal(opts Options, article []byte) (err error) {
	paths := finalPublicationPaths(opts)
	if len(paths) == 0 {
		return errors.New("digest: final publication requires --out or an artifact directory")
	}
	outStage, err := reservePublicationStage(paths[0])
	if err != nil {
		return fmt.Errorf("digest: stage final output %q: %w", paths[0], err)
	}
	defer func() { _ = os.Remove(outStage) }()
	if writeErr := writeCheckpoint(opts, "final output staging", outStage, article); writeErr != nil {
		return writeErr
	}
	if len(paths) == 1 {
		if renameErr := opts.renameFile(outStage, paths[0]); renameErr != nil {
			return fmt.Errorf("digest: publish final output %q: %w", paths[0], renameErr)
		}
		return nil
	}
	artifactStage, err := reservePublicationStage(paths[1])
	if err != nil {
		return fmt.Errorf("digest: stage final artifact %q: %w", paths[1], err)
	}
	defer func() { _ = os.Remove(artifactStage) }()
	if err := writeCheckpoint(opts, "final artifact staging", artifactStage, article); err != nil {
		return err
	}
	if err := opts.renameFile(outStage, paths[0]); err != nil {
		return fmt.Errorf("digest: publish final output %q: %w", paths[0], err)
	}
	if err := opts.renameFile(artifactStage, paths[1]); err != nil {
		rollbackErr := os.Remove(paths[0])
		if rollbackErr != nil && !errors.Is(rollbackErr, fs.ErrNotExist) {
			return errors.Join(
				fmt.Errorf("digest: publish final artifact %q: %w", paths[1], err),
				fmt.Errorf("digest: rollback final output %q: %w", paths[0], rollbackErr),
			)
		}
		return fmt.Errorf("digest: publish final artifact %q; rolled back final output: %w", paths[1], err)
	}
	return nil
}
