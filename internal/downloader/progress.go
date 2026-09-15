package downloader

import "io"

// Progress describes bytes already written to the atomic output.
type Progress struct {
	BytesDownloaded int64
	TotalBytes      int64
	TotalBytesKnown bool
	SegmentIndex    int
	SegmentTotal    int
}

// ProgressCallback receives truthful download progress and may stop the download.
type ProgressCallback func(Progress) error

type progressState struct {
	callback        ProgressCallback
	bytesDownloaded int64
	totalBytes      int64
	totalKnown      bool
	segmentIndex    int
	segmentTotal    int
}

type progressWriter struct {
	destination io.Writer
	state       *progressState
}

func (w progressWriter) Write(chunk []byte) (int, error) {
	written, err := w.destination.Write(chunk)
	if written == 0 {
		return written, err
	}
	w.state.bytesDownloaded += int64(written)
	if w.state.callback == nil {
		return written, err
	}
	callbackErr := w.state.callback(Progress{
		BytesDownloaded: w.state.bytesDownloaded,
		TotalBytes:      w.state.totalBytes,
		TotalBytesKnown: w.state.totalKnown,
		SegmentIndex:    w.state.segmentIndex,
		SegmentTotal:    w.state.segmentTotal,
	})
	if err == nil && callbackErr != nil {
		err = callbackErr
	}
	return written, err
}
