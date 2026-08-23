package fileops

import (
	"os"

	"github.com/kiruva/tyr/internal/remote"
)

// runRemote dispatches the ssh-backed operations. Move semantics are "transfer
// then remove the source", and the removal only happens once the transfer has
// reported success.
func runRemote(job Job, r *reporter) error {
	switch job.Op {
	case OpDownload:
		if err := remote.Download(job.Host, job.Srcs, job.Dest, r.step); err != nil {
			return err
		}
		if job.Move {
			return remote.Delete(job.Host, job.Srcs, func(string) {})
		}
		return nil

	case OpUpload:
		if err := remote.Upload(job.Host, job.Srcs, job.Dest, r.step); err != nil {
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
		return remote.Transfer(job.Host, job.Srcs, job.Dest, job.Move, r.step)

	case OpRemoteDelete:
		return remote.Delete(job.Host, job.Srcs, r.step)
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
