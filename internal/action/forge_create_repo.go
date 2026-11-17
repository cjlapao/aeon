package action

// forge_create_repo.go is `forge.createRepo` — GI10 end to end, behind
// the person door. THREE KEYS turn before a request is shaped, in this
// order, and every one of them is asked BEFORE any bytes leave the
// process: the S44 setting (intake.allowRemoteCreate, default false), the
// S56 autonomy grant (the fixed sentence until the grant seam is wired),
// and the person's own publish answer (private is the only way to a
// private repository; public without confirm_public is a refusal naming
// the field). What follows is a check-then-act: GET before POST, never
// retarget somebody's remote, never force, and a ForgeError travels
// verbatim — the three-part sentence is the forge's, not a paraphrase.

import (
	"context"
	"fmt"

	"github.com/cjlapao/aeon/internal/forge"
)

// CreateRepoActions is the daemon's gate wiring. The setting seam reads
// the declared S44 key through config.Reader (the store never enters this
// package); the grant seam is S56's verdict for root action
// forge.createRepo — while S56 has no grant wired, the daemon's seam
// answers the fixed sentence and nothing ships.
type CreateRepoActions struct {
	SettingEnabled func(ctx context.Context) (bool, error)
	Grant          func(ctx context.Context) error
}

func runCreateRepo(ctx context.Context, svc *Services, a Args) (map[string]string, error) {
	// --- key 1: the declared setting, default deny --------------------
	if svc.CreateRepo == nil || svc.CreateRepo.SettingEnabled == nil {
		return nil, fmt.Errorf("forge.createRepo refuses: the intake.allowRemoteCreate seam is not wired — the S44 setting decides, its default is false, and an unwired seam is that default")
	}
	allowed, err := svc.CreateRepo.SettingEnabled(ctx)
	if err != nil {
		return nil, fmt.Errorf("forge.createRepo refuses: intake.allowRemoteCreate could not be read: %v", err)
	}
	if !allowed {
		return nil, fmt.Errorf("forge.createRepo refuses: intake.allowRemoteCreate is off — a person must switch it on (S44, the settings door) before any repository is created")
	}

	// --- key 2: the autonomy grant (S56 R3) ---------------------------
	if svc.CreateRepo.Grant == nil {
		return nil, fmt.Errorf("remote repository creation needs an autonomy grant (S56)")
	}
	if err := svc.CreateRepo.Grant(ctx); err != nil {
		// Verbatim: the grant's own sentence, with its own alternatives.
		return nil, err
	}

	// --- key 3: the person's publish answer ---------------------------
	private := true
	switch a.Publish {
	case "private":
	case "public":
		private = false
	default:
		return nil, fmt.Errorf("forge.createRepo refuses: publish must be the person's own answer — \"private\" or \"public\" — and %q is neither", a.Publish)
	}
	if !private && !a.ConfirmPublic {
		return nil, fmt.Errorf("forge.createRepo refuses: publishing PUBLIC needs confirm_public — a repository is outward publication of its content, and private is the default")
	}

	// --- the instruments ------------------------------------------------
	if svc.Forge == nil {
		return nil, fmt.Errorf("forge.createRepo: no forge wired")
	}
	creator, canCreate := svc.Forge.Forge.(forge.RepoCreator)
	if !canCreate {
		return nil, fmt.Errorf("forge.createRepo refuses: this forge cannot create repositories — the capability is absent, not off")
	}
	reader, canRead := svc.Forge.Forge.(forge.RepoReader)
	if !canRead {
		return nil, fmt.Errorf("forge.createRepo refuses: this forge cannot check whether a repository already exists — and createRepo never POSTs blind (GI10: check, then act)")
	}
	if svc.Git == nil {
		return nil, fmt.Errorf("forge.createRepo: no git client wired")
	}
	gitc := svc.Git.client()

	req := forge.RepoRequest{
		Owner: a.Owner, Name: a.RepoName, Private: private,
		Description: a.Description, OwnerKind: a.OwnerKind,
	}
	// The closed name rules run HERE, before the first network word (the
	// forge re-checks; the refusal is reached locally).
	if err := req.Validate(); err != nil {
		return nil, err // the ForgeError's own three-part sentence
	}

	// --- check, then act -------------------------------------------------
	existing, err := reader.GetRepo(ctx, a.Owner, a.RepoName)
	if err != nil {
		return nil, err // verbatim — auth, refused, transport, all three-part already
	}
	var repo *forge.Repo
	if existing != nil {
		switch {
		case !existing.Empty:
			// The resume half of the strategy: a re-run whose OWN push is
			// already on the remote answers the same evidence and calls
			// nothing else (behavior 6); anything else under that name is
			// somebody else's — a conflict, and a stop.
			cur, has := func() (string, bool) {
				u, ok, err := gitc.RemoteURL(ctx, a.Dir, "origin")
				if err != nil || !ok {
					return "", false
				}
				return u, true
			}()
			head, headErr := gitc.LocalHEAD(ctx, a.Dir)
			if has && cur == existing.CloneURL && headErr == nil {
				if sha, err := gitc.RemoteRefSHA(ctx, a.Dir, existing.DefaultBranch); err == nil && sha == head {
					return mustEvidence(ForgeCreateRepo, map[string]string{
						"repo_url": existing.URL, "default_branch": existing.DefaultBranch, "pushed_sha": sha,
					}), nil
				}
			}
			return nil, fmt.Errorf("forge.createRepo refuses: %s/%s already exists and holds commits this run did not write — a taken name is a conflict, not a suggestion, and nothing is pushed at it", a.Owner, a.RepoName)
		case !a.AdoptExisting:
			return nil, fmt.Errorf("forge.createRepo refuses: %s/%s already exists and is EMPTY — adopting it is a deliberate act and needs adopt_existing; the default is refusal", a.Owner, a.RepoName)
		default:
			repo = existing // the person said adopt; an empty repository is an empty canvas, said aloud above
		}
	} else {
		created, err := creator.CreateRepo(ctx, req)
		if err != nil {
			return nil, err // verbatim: "name already exists" arrives as the forge's conflict sentence
		}
		repo = created
	}

	branch := repo.DefaultBranch
	if branch == "" {
		b, err := gitc.CurrentBranch(ctx, a.Dir)
		if err != nil {
			return nil, fmt.Errorf("forge.createRepo: the repository names no default branch and the local branch could not be read: %w", err)
		}
		branch = b
	}

	// origin is checked before it is touched and NEVER retargeted (GI10).
	url, has, err := gitc.RemoteURL(ctx, a.Dir, "origin")
	if err != nil {
		return nil, fmt.Errorf("forge.createRepo: reading origin: %w", err)
	}
	if has {
		if url != repo.CloneURL {
			return nil, fmt.Errorf("forge.createRepo refuses: origin already points at %s, not %s — publish never retargets a remote somebody else chose", url, repo.CloneURL)
		}
	} else if err := gitc.AddRemote(ctx, a.Dir, "origin", repo.CloneURL); err != nil {
		return nil, fmt.Errorf("forge.createRepo: adding origin: %w", err)
	}

	if err := gitc.Push(ctx, a.Dir, branch); err != nil {
		return nil, fmt.Errorf("forge.createRepo: pushing %s: %w", branch, err)
	}
	pushed, err := gitc.LocalHEAD(ctx, a.Dir)
	if err != nil {
		return nil, fmt.Errorf("forge.createRepo: reading the pushed head: %w", err)
	}
	return mustEvidence(ForgeCreateRepo, map[string]string{
		"repo_url":       repo.URL,
		"default_branch": branch,
		"pushed_sha":     pushed,
	}), nil
}
