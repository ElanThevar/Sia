package siatest

import (
	"fmt"
	"math"
	"path/filepath"
	"strconv"
	"time"

	"github.com/NebulousLabs/Sia/crypto"
	"github.com/NebulousLabs/Sia/modules"
	"github.com/NebulousLabs/Sia/node/api"

	"github.com/NebulousLabs/errors"
	"github.com/NebulousLabs/fastrand"
)

// DownloadToDisk downloads a previously uploaded file. The file will be downloaded
// to a random location and returned as a TestFile object.
func (tn *TestNode) DownloadToDisk(rf *RemoteFile, async bool) (*LocalFile, error) {
	fi, err := tn.FileInfo(rf)
	if err != nil {
		return nil, errors.AddContext(err, "failed to retrieve FileInfo")
	}
	// Create a random destination for the download
	fileName := strconv.Itoa(fastrand.Intn(math.MaxInt32))
	dest := filepath.Join(SiaTestingDir, fileName)
	if err := tn.RenterDownloadGet(rf.siaPath, dest, 0, fi.Filesize, async); err != nil {
		return nil, errors.AddContext(err, "failed to download file")
	}
	// Create the TestFile
	lf := &LocalFile{
		path:     dest,
		checksum: rf.checksum,
	}
	// If we download the file asynchronously we are done
	if async {
		return lf, nil
	}
	// Verify checksum if we downloaded the file blocking
	if err := lf.checkIntegrity(); err != nil {
		return lf, errors.AddContext(err, "downloaded file's checksum doesn't match")
	}
	return lf, nil
}

// DownloadByStream downloads a file and returns its contents as a slice of bytes.
func (tn *TestNode) DownloadByStream(rf *RemoteFile) (data []byte, err error) {
	fi, err := tn.FileInfo(rf)
	if err != nil {
		return nil, errors.AddContext(err, "failed to retrieve FileInfo")
	}
	data, err = tn.RenterDownloadHTTPResponseGet(rf.siaPath, 0, fi.Filesize)
	if err == nil && rf.checksum != crypto.HashAll(data) {
		err = errors.New("downloaded bytes don't match requested data")
	}
	return
}

// DownloadInfo returns the DownloadInfo struct of a file. If it returns nil,
// the download has either finished, or was never started in the first place.
// If the corresponding download info was found, DownloadInfo also performs a
// few sanity checks on its fields.
func (tn *TestNode) DownloadInfo(lf *LocalFile, rf *RemoteFile) (*api.DownloadInfo, error) {
	rdq, err := tn.RenterDownloadsGet()
	if err != nil {
		return nil, err
	}
	var di *api.DownloadInfo
	for _, d := range rdq.Downloads {
		if rf.siaPath == d.SiaPath && lf.path == d.Destination {
			di = &d
			break
		}
	}
	if di == nil {
		// No download info found.
		return nil, errors.New("download info not found")
	}
	// Check if length and filesize were set correctly
	if di.Length != di.Filesize {
		err = errors.AddContext(err, "filesize != length")
	}
	// Received data can't be larger than transfered data
	if di.Received > di.TotalDataTransferred {
		err = errors.AddContext(err, "received > TotalDataTransfered")
	}
	// If the download is completed, the amount of received data has to equal
	// the amount of requested data.
	if di.Completed && di.Received != di.Length {
		err = errors.AddContext(err, "completed == true but received != length")
	}
	return di, err
}

// Files lists the files tracked by the renter
func (tn *TestNode) Files() ([]modules.FileInfo, error) {
	rf, err := tn.RenterFilesGet()
	if err != nil {
		return nil, err
	}
	return rf.Files, err
}

// FileInfo retrieves the info of a certain file that is tracked by the renter
func (tn *TestNode) FileInfo(rf *RemoteFile) (modules.FileInfo, error) {
	files, err := tn.Files()
	if err != nil {
		return modules.FileInfo{}, err
	}
	for _, file := range files {
		if file.SiaPath == rf.siaPath {
			return file, nil
		}
	}
	return modules.FileInfo{}, errors.New("file is not tracked by the renter")
}

// Upload uses the node to upload the file.
func (tn *TestNode) Upload(lf *LocalFile, dataPieces, parityPieces uint64) (*RemoteFile, error) {
	// Upload file
	err := tn.RenterUploadPost(lf.path, "/"+lf.fileName(), dataPieces, parityPieces)
	if err != nil {
		return nil, err
	}
	// Create remote file object
	rf := &RemoteFile{
		siaPath:  lf.fileName(),
		checksum: lf.checksum,
	}
	// Make sure renter tracks file
	_, err = tn.FileInfo(rf)
	if err != nil {
		return rf, errors.AddContext(err, "uploaded file is not tracked by the renter")
	}
	return rf, nil
}

// UploadNewFile initiates the upload of a filesize bytes large file.
func (tn *TestNode) UploadNewFile(filesize int, dataPieces uint64, parityPieces uint64) (rf *RemoteFile, err error) {
	// Create file for upload
	lf, err := NewFile(filesize)
	if err != nil {
		err = errors.AddContext(err, "failed to create file")
		return
	}
	// Upload file, creating a parity piece for each host in the group
	rf, err = tn.Upload(lf, dataPieces, parityPieces)
	if err != nil {
		err = errors.AddContext(err, "failed to start upload")
		return
	}
	return
}

// UploadNewFileBlocking uploads a filesize bytes large file and waits for the
// upload to reach 100% progress and redundancy.
func (tn *TestNode) UploadNewFileBlocking(filesize int, dataPieces uint64, parityPieces uint64) (rf *RemoteFile, err error) {
	rf, err = tn.UploadNewFile(filesize, dataPieces, parityPieces)
	if err != nil {
		return
	}
	// Wait until upload reached the specified progress
	if err = tn.WaitForUploadProgress(rf, 1); err != nil {
		return
	}
	// Wait until upload reaches a certain redundancy
	err = tn.WaitForUploadRedundancy(rf, float64((dataPieces+parityPieces))/float64(dataPieces))
	return
}

// WaitForDownload waits for the download of a file to finish. If a file wasn't
// scheduled for download it will return instantly without an error. If parent
// is provided, it will compare the contents of the downloaded file to the
// contents of tf2 after the download is finished. WaitForDownload also
// verifies the checksum of the downloaded file.
func (tn *TestNode) WaitForDownload(lf *LocalFile, rf *RemoteFile) error {
	err := Retry(1000, 100*time.Millisecond, func() error {
		file, err := tn.DownloadInfo(lf, rf)
		if err != nil {
			return errors.AddContext(err, "couldn't retrieve DownloadInfo")
		}
		if file == nil {
			return nil
		}
		if !file.Completed {
			return errors.New("download hasn't finished yet")
		}
		return nil
	})
	if err != nil {
		return err
	}
	// Verify checksum
	return lf.checkIntegrity()
}

// WaitForUploadProgress waits for a file to reach a certain upload progress.
func (tn *TestNode) WaitForUploadProgress(rf *RemoteFile, progress float64) error {
	if _, err := tn.FileInfo(rf); err != nil {
		return errors.New("file is not tracked by renter")
	}
	// Wait until it reaches the progress
	return Retry(1000, 100*time.Millisecond, func() error {
		file, err := tn.FileInfo(rf)
		if err != nil {
			return errors.AddContext(err, "couldn't retrieve FileInfo")
		}
		if file.UploadProgress < progress {
			return fmt.Errorf("progress should be %v but was %v", progress, file.UploadProgress)
		}
		return nil
	})

}

// WaitForUploadRedundancy waits for a file to reach a certain upload redundancy.
func (tn *TestNode) WaitForUploadRedundancy(rf *RemoteFile, redundancy float64) error {
	// Check if file is tracked by renter at all
	if _, err := tn.FileInfo(rf); err != nil {
		return errors.New("file is not tracked by renter")
	}
	// Wait until it reaches the redundancy
	return Retry(1000, 100*time.Millisecond, func() error {
		file, err := tn.FileInfo(rf)
		if err != nil {
			return errors.AddContext(err, "couldn't retrieve FileInfo")
		}
		if file.Redundancy < redundancy {
			return fmt.Errorf("redundancy should be %v but was %v", redundancy, file.Redundancy)
		}
		return nil
	})
}
