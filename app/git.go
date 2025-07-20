package app

import (
	"fmt"
	"io"
	"strings"

	"github.com/aymanbagabas/go-udiff"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
)

// GitRepo provides a wrapper around a go-git repository, simplifying git operations.
type GitRepo struct {
	repo *git.Repository
}

// RefInfo holds information about a git reference, such as a branch or commit.
type RefInfo struct {
	Name   string
	Hash   string
	Type   string // "branch" or "commit"
	IsHead bool
}

// OpenRepo opens a git repository at the given path.
func OpenRepo(path string) (*GitRepo, error) {
	repo, err := git.PlainOpen(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open git repository: %w", err)
	}

	return &GitRepo{repo: repo}, nil
}

// GetBranches returns a list of all local branches in the repository.
func (g *GitRepo) GetBranches() ([]RefInfo, error) {
	branches, err := g.repo.Branches()
	if err != nil {
		return nil, fmt.Errorf("failed to get branches: %w", err)
	}

	var refs []RefInfo
	err = branches.ForEach(func(ref *plumbing.Reference) error {
		name := ref.Name().Short()
		refs = append(refs, RefInfo{
			Name: name,
			Hash: ref.Hash().String()[:8],
			Type: "branch",
		})
		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to iterate branches: %w", err)
	}

	return refs, nil
}

// GetRecentCommits returns a list of the most recent commits from the current HEAD.
func (g *GitRepo) GetRecentCommits(limit int) ([]RefInfo, error) {
	head, err := g.repo.Head()
	if err != nil {
		return nil, fmt.Errorf("failed to get HEAD: %w", err)
	}

	commits, err := g.repo.Log(&git.LogOptions{
		From: head.Hash(),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get commits: %w", err)
	}

	var refs []RefInfo
	count := 0
	err = commits.ForEach(func(commit *object.Commit) error {
		if count >= limit {
			return fmt.Errorf("limit reached")
		}

		message := strings.Split(commit.Message, "\n")[0]
		if len(message) > 50 {
			message = message[:47] + "..."
		}

		refs = append(refs, RefInfo{
			Name: fmt.Sprintf("%s - %s", message, commit.Hash.String()[:8]),
			Hash: commit.Hash.String(),
			Type: "commit",
		})
		count++
		return nil
	})

	if err != nil && err.Error() != "limit reached" {
		return nil, fmt.Errorf("failed to iterate commits: %w", err)
	}

	return refs, nil
}

// GetDiff generates a diff between two git references.
func (g *GitRepo) GetDiff(from, to string) (string, error) {
	fromHash, err := g.repo.ResolveRevision(plumbing.Revision(from))
	if err != nil {
		return "", fmt.Errorf("failed to resolve 'from' revision %s: %w", from, err)
	}

	toHash, err := g.repo.ResolveRevision(plumbing.Revision(to))
	if err != nil {
		return "", fmt.Errorf("failed to resolve 'to' revision %s: %w", to, err)
	}

	fromCommit, err := g.repo.CommitObject(*fromHash)
	if err != nil {
		return "", fmt.Errorf("failed to get 'from' commit: %w", err)
	}

	toCommit, err := g.repo.CommitObject(*toHash)
	if err != nil {
		return "", fmt.Errorf("failed to get 'to' commit: %w", err)
	}

	fromTree, err := fromCommit.Tree()
	if err != nil {
		return "", fmt.Errorf("failed to get 'from' tree: %w", err)
	}

	toTree, err := toCommit.Tree()
	if err != nil {
		return "", fmt.Errorf("failed to get 'to' tree: %w", err)
	}

	changes, err := object.DiffTree(fromTree, toTree)
	if err != nil {
		return "", fmt.Errorf("failed to diff trees: %w", err)
	}

	var diff strings.Builder
	for _, change := range changes {
		patch, err := change.Patch()
		if err != nil {
			continue
		}
		diff.WriteString(patch.String())
	}

	return diff.String(), nil
}


// GetStagedFileDiffs returns individual file diffs for staged changes
func (g *GitRepo) GetStagedFileDiffs() ([]FileDiff, error) {
	hasStagedChanges, err := g.HasStagedChanges()
	if err != nil {
		return nil, err
	}

	if !hasStagedChanges {
		return nil, nil
	}

	stagedFiles, err := g.GetStagedFilePaths()
	if err != nil {
		return nil, fmt.Errorf("failed to get staged files: %w", err)
	}

	var fileDiffs []FileDiff
	for _, filename := range stagedFiles {
		edits, err := g.GetStagedFileEdits(filename)
		if err != nil {
			continue // Skip files we can't diff
		}
		
		if len(edits) > 0 {
			unifiedDiff := g.formatEditsAsUnifiedDiff(edits, filename)
			fileDiffs = append(fileDiffs, FileDiff{
				Filename: filename,
				Content:  unifiedDiff,
				Edits:    edits,
			})
		}
	}

	return fileDiffs, nil
}

// GetStagedDiff generates a diff between HEAD and staged files
func (g *GitRepo) GetStagedDiff() (string, error) {
	hasStagedChanges, err := g.HasStagedChanges()
	if err != nil {
		return "", err
	}

	if !hasStagedChanges {
		return "", nil
	}

	stagedFiles, err := g.GetStagedFilePaths()
	if err != nil {
		return "", fmt.Errorf("failed to get staged files: %w", err)
	}

	var diffBuilder strings.Builder
	for _, filename := range stagedFiles {
		edits, err := g.GetStagedFileEdits(filename)
		if err != nil {
			continue // Skip files we can't diff
		}
		
		if len(edits) > 0 {
			unifiedDiff := g.formatEditsAsUnifiedDiff(edits, filename)
			diffBuilder.WriteString(unifiedDiff)
		}
	}

	return diffBuilder.String(), nil
}

// HasStagedChanges checks if there are any staged changes in the repository.
func (g *GitRepo) HasStagedChanges() (bool, error) {
	worktree, err := g.repo.Worktree()
	if err != nil {
		return false, fmt.Errorf("failed to get worktree: %w", err)
	}

	status, err := worktree.Status()
	if err != nil {
		return false, fmt.Errorf("failed to get status: %w", err)
	}

	for _, fileStatus := range status {
		if fileStatus.Staging != '?' && fileStatus.Staging != ' ' {
			return true, nil
		}
	}

	return false, nil
}

// GetStagedRef returns a special RefInfo for staged files if any exist.
func (g *GitRepo) GetStagedRef() (*RefInfo, error) {
	hasStagedChanges, err := g.HasStagedChanges()
	if err != nil {
		return nil, err
	}

	if !hasStagedChanges {
		return nil, nil
	}

	return &RefInfo{
		Name: "Staged Changes",
		Hash: "staged",
		Type: "staged",
	}, nil
}

// GetUnstagedFileDiffs returns individual file diffs for unstaged changes
func (g *GitRepo) GetUnstagedFileDiffs() ([]FileDiff, error) {
	unstagedFiles, err := g.GetUnstagedFilePaths()
	if err != nil {
		return nil, fmt.Errorf("failed to get unstaged files: %w", err)
	}

	if len(unstagedFiles) == 0 {
		return nil, nil
	}

	var fileDiffs []FileDiff
	for _, filename := range unstagedFiles {
		edits, err := g.GetUnstagedFileEdits(filename)
		if err != nil {
			continue // Skip files we can't diff
		}
		
		if len(edits) > 0 {
			unifiedDiff := g.formatEditsAsUnifiedDiff(edits, filename)
			fileDiffs = append(fileDiffs, FileDiff{
				Filename: filename,
				Content:  unifiedDiff,
				Edits:    edits,
			})
		}
	}

	return fileDiffs, nil
}

// GetUnstagedDiff generates a diff of unstaged changes (working tree vs HEAD).
func (g *GitRepo) GetUnstagedDiff() (string, error) {
	unstagedFiles, err := g.GetUnstagedFilePaths()
	if err != nil {
		return "", fmt.Errorf("failed to get unstaged files: %w", err)
	}

	if len(unstagedFiles) == 0 {
		return "", nil
	}

	var diffBuilder strings.Builder
	for _, filename := range unstagedFiles {
		edits, err := g.GetUnstagedFileEdits(filename)
		if err != nil {
			continue // Skip files we can't diff
		}
		
		if len(edits) > 0 {
			unifiedDiff := g.formatEditsAsUnifiedDiff(edits, filename)
			diffBuilder.WriteString(unifiedDiff)
		}
	}

	return diffBuilder.String(), nil
}

// Returns slice of file paths for staged files
func (g *GitRepo) GetStagedFilePaths() ([]string, error) {
	worktree, err := g.repo.Worktree()
	if err != nil {
		return nil, fmt.Errorf("failed to get worktree: %w", err)
	}

	status, err := worktree.Status()
	if err != nil {
		return nil, fmt.Errorf("failed to get status: %w", err)
	}

	var stagedFiles []string
	for file, fileStatus := range status {
		if fileStatus.Staging != '?' && fileStatus.Staging != ' ' {
			stagedFiles = append(stagedFiles, file)
		}
	}

	return stagedFiles, nil
}

func (g *GitRepo) GetUnstagedFilePaths() ([]string, error) {
	worktree, err := g.repo.Worktree()
	if err != nil {
		return nil, fmt.Errorf("failed to get worktree: %w", err)
	}

	status, err := worktree.Status()
	if err != nil {
		return nil, fmt.Errorf("failed to get status: %w", err)
	}

	var unstagedFiles []string
	for file, fileStatus := range status {
		// Check if file has unstaged changes
		if fileStatus.Worktree != '?' && fileStatus.Worktree != ' ' {
			unstagedFiles = append(unstagedFiles, file)
		}
	}

	return unstagedFiles, nil
}

func (g *GitRepo) GetStagedFileContent(filename string) (string, error) {
	index, err := g.repo.Storer.Index()
	if err != nil {
		return "", fmt.Errorf("failed to get index: %w", err)
	}

	entry, err := index.Entry(filename)
	if err != nil {
		return "", fmt.Errorf("file not found in index: %w", err)
	}

	blob, err := g.repo.BlobObject(entry.Hash)
	if err != nil {
		return "", fmt.Errorf("failed to get blob: %w", err)
	}

	reader, err := blob.Reader()
	if err != nil {
		return "", fmt.Errorf("failed to get blob reader: %w", err)
	}
	defer reader.Close()

	content, err := io.ReadAll(reader)
	if err != nil {
		return "", fmt.Errorf("failed to read blob content: %w", err)
	}

	return string(content), nil
}

func (g *GitRepo) GetWorkingTreeFileContent(filename string) (string, error) {
	worktree, err := g.repo.Worktree()
	if err != nil {
		return "", fmt.Errorf("failed to get worktree: %w", err)
	}

	file, err := worktree.Filesystem.Open(filename)
	if err != nil {
		return "", fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	content, err := io.ReadAll(file)
	if err != nil {
		return "", fmt.Errorf("failed to read file: %w", err)
	}

	return string(content), nil
}

func (g *GitRepo) GetHEADFileContent(filename string) (string, error) {
	head, err := g.repo.Head()
	if err != nil {
		return "", fmt.Errorf("failed to get HEAD: %w", err)
	}
	commit := head.Hash().String()
	content, err := g.getFileContentFromCommit(commit, filename)

	if err != nil {
		return "", fmt.Errorf("failed to get HEAD file contents %w", err)
	}

	return content, nil
}

func (g *GitRepo) getFileContentFromCommit(commitHash, filename string) (string, error) {
	hash := plumbing.NewHash(commitHash)

	commit, err := g.repo.CommitObject(hash)
	if err != nil {
		return "", fmt.Errorf("failed to get commit: %w", err)
	}

	tree, err := commit.Tree()
	if err != nil {
		return "", fmt.Errorf("failed to get commit tree: %w", err)
	}

	file, err := tree.File(filename)
	if err != nil {
		return "", fmt.Errorf("file not found in commit: %w", err)
	}

	content, err := file.Contents()
	if err != nil {
		return "", fmt.Errorf("failed to read file contents: %w", err)
	}

	return content, nil
}

// GetStagedFileEdits returns udiff.Edit operations for a staged file compared to HEAD
func (g *GitRepo) GetStagedFileEdits(filename string) ([]udiff.Edit, error) {
	headContent, err := g.GetHEADFileContent(filename)
	if err != nil {
		// File might be new, use empty content for HEAD
		headContent = ""
	}

	stagedContent, err := g.GetStagedFileContent(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to get staged file content: %w", err)
	}

	edits := udiff.Strings(headContent, stagedContent)
	return edits, nil
}

// GetUnstagedFileEdits returns udiff.Edit operations for an unstaged file compared to HEAD
func (g *GitRepo) GetUnstagedFileEdits(filename string) ([]udiff.Edit, error) {
	headContent, err := g.GetHEADFileContent(filename)
	if err != nil {
		// File might be new, use empty content for HEAD
		headContent = ""
	}

	workingTreeContent, err := g.GetWorkingTreeFileContent(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to get working tree file content: %w", err)
	}

	edits := udiff.Strings(headContent, workingTreeContent)
	return edits, nil
}

// GetFileEdits returns udiff.Edit operations between two versions of a file
func (g *GitRepo) GetFileEdits(filename, fromCommit, toCommit string) ([]udiff.Edit, error) {
	fromContent, err := g.getFileContentFromCommit(fromCommit, filename)
	if err != nil {
		// File might not exist in from commit, use empty content
		fromContent = ""
	}

	toContent, err := g.getFileContentFromCommit(toCommit, filename)
	if err != nil {
		// File might not exist in to commit, use empty content
		toContent = ""
	}

	edits := udiff.Strings(fromContent, toContent)
	return edits, nil
}

// formatEditsAsUnifiedDiff converts udiff.Edit operations back to unified diff format
func (g *GitRepo) formatEditsAsUnifiedDiff(edits []udiff.Edit, filename string) string {
	if len(edits) == 0 {
		return ""
	}

	// Get the original content to apply edits against
	headContent, err := g.GetHEADFileContent(filename)
	if err != nil {
		// File might be new, use empty content
		headContent = ""
	}

	// Use udiff's ToUnified function
	oldLabel := "a/" + filename
	newLabel := "b/" + filename
	unifiedDiff, err := udiff.ToUnified(oldLabel, newLabel, headContent, edits, 3)
	if err != nil {
		return ""
	}

	return unifiedDiff
}

// GetPrimaryFilename returns the first filename from a list of FileDiffs, or a default
func GetPrimaryFilename(fileDiffs []FileDiff) string {
	if len(fileDiffs) == 0 {
		return "mixed_files"
	}
	return fileDiffs[0].Filename
}

// CombineFileDiffs combines multiple FileDiff objects into a single unified diff string
func CombineFileDiffs(fileDiffs []FileDiff) string {
	if len(fileDiffs) == 0 {
		return ""
	}
	
	var combined strings.Builder
	for _, fileDiff := range fileDiffs {
		combined.WriteString(fileDiff.Content)
	}
	
	return combined.String()
}
