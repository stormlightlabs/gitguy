package app

import (
	"fmt"
	"strings"

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

// GetWorkingTreeDiff generates a diff of the current working tree against the HEAD.
func (g *GitRepo) GetWorkingTreeDiff() (string, error) {
	head, err := g.repo.Head()
	if err != nil {
		return "", fmt.Errorf("failed to get HEAD: %w", err)
	}

	headCommit, err := g.repo.CommitObject(head.Hash())
	if err != nil {
		return "", fmt.Errorf("failed to get HEAD commit: %w", err)
	}

	headTree, err := headCommit.Tree()
	if err != nil {
		return "", fmt.Errorf("failed to get HEAD tree: %w", err)
	}

	changes, err := object.DiffTree(headTree, nil)
	if err != nil {
		return "", fmt.Errorf("failed to diff working tree: %w", err)
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
