package fileops

import (
	"context"
	"os"

	"github.com/kiruva/tyr/internal/remote"
)

// runRemote dispatches the ssh-backed operations. Move semantics are "transfer
// then remove the source", and the removal only happens once the transfer has
// reported success.
func runRemote(ctx context.Context, job Job, r *reporter) error {
	switch job.Op {
	case OpDownload:
		if err := remote.Download(ctx, job.Host, job.Srcs, job.Dest, r.step); err != nil {
			return err
		}
		if job.Move {
			return remote.Delete(ctx, job.Host, job.Srcs, func(string) {})
		}
		return nil

	case OpUpload:
		if err := remote.Upload(ctx, job.Host, job.Srcs, job.Dest, r.step); err != nil {
			return err
		}
		if job.Move {
			for _, s := range job.Srcs {
				if err := os.RemoveAll(s); err != nil {
					return err
				}
			}
		}
		return nil

	case OpRemoteCopy:
		return remote.Transfer(ctx, job.Host, job.Srcs, job.Dest, job.Move, r.step)

	case OpRemoteDelete:
		return remote.Delete(ctx, job.Host, job.Srcs, r.step)
	}
	return nil
}

// isRemoteOp reports whether the op runs over ssh.
func isRemoteOp(op Op) bool {
	switch op {
	case OpDownload, OpUpload, OpRemoteCopy, OpRemoteDelete:
		return true
	}
	return false
}
