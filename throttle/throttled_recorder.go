// thermal-recorder - record thermal video footage of warm moving objects
//  Copyright (C) 2018, The Cacophony Project
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU General Public License for more details.
//
// You should have received a copy of the GNU General Public License
// along with this program. If not, see <http://www.gnu.org/licenses/>.

package throttle

import (
	"log"
	"time"

	"github.com/TheCacophonyProject/lepton3"
	"github.com/juju/ratelimit"

	"github.com/TheCacophonyProject/thermal-recorder/recorder"
)

func NewThrottledRecorder(
	baseRecorder recorder.Recorder,
	config *ThrottlerConfig,
	minSeconds int,
	eventListener ThrottledEventListener,
) *ThrottledRecorder {
	return NewThrottledRecorderWithClock(
		baseRecorder,
		config,
		minSeconds,
		eventListener,
		new(realClock),
	)
}

func NewThrottledRecorderWithClock(
	baseRecorder recorder.Recorder,
	config *ThrottlerConfig,
	minSeconds int,
	listener ThrottledEventListener,
	clock ratelimit.Clock,
) *ThrottledRecorder {

	// The token bucket tracks the number of *frames* available for recording.
	bucketFrames := int64(config.ThrottleAfter) * lepton3.FramesHz
	minFrames := minSeconds * lepton3.FramesHz
	refillRate := float64(minFrames) / config.MinRefill.Seconds()
	bucket := ratelimit.NewBucketWithRateAndClock(refillRate, bucketFrames, clock)
	if listener == nil {
		listener = new(nullListener)
	}
	return &ThrottledRecorder{
		recorder:           baseRecorder,
		listener:           listener,
		bucket:             bucket,
		minRecordingLength: int64(minSeconds) * lepton3.FramesHz,
	}
}

// ThrottledRecorder wraps a standard recorder so that it stops
// recording (ie gets throttled) if requested to record too often.
// This is desirable as the extra recordings are likely to be highly
// similar to the earlier recordings and contain no new information.
// It can happen when an animal is stuck in a trap or it is very
// windy.
type ThrottledRecorder struct {
	recorder           recorder.Recorder
	listener           ThrottledEventListener
	bucket             *ratelimit.Bucket
	recording          bool
	minRecordingLength int64
	throttledFrames    uint32
	totalFrames        uint32
}

type ThrottledEventListener interface {
	WhenThrottled()
}

type nullListener struct{}

func (lis *nullListener) WhenThrottled() {}

func (throttler *ThrottledRecorder) CheckCanRecord() error {
	return throttler.recorder.CheckCanRecord()
}

func (throttler *ThrottledRecorder) StartRecording() error {
	if throttler.bucket.Available() >= throttler.minRecordingLength {
		throttler.recording = true
		return throttler.recorder.StartRecording()
	} else {
		throttler.recording = false
		log.Print("Recording not started due to throttling")
		throttler.listener.WhenThrottled()
		return nil
	}
}

func (throttler *ThrottledRecorder) StopRecording() error {
	if throttler.recording && throttler.throttledFrames > 0 {
		log.Printf("Stop recording; %d/%d frames throttled", throttler.throttledFrames, throttler.totalFrames)
	}
	throttler.throttledFrames = 0
	throttler.totalFrames = 0

	if throttler.recording {
		throttler.recording = false
		return throttler.recorder.StopRecording()
	}
	return nil
}

func (throttler *ThrottledRecorder) WriteFrame(frame *lepton3.Frame) error {
	if !throttler.recording {
		return nil
	}

	throttler.totalFrames++
	if throttler.bucket.TakeAvailable(1) > 0 {
		return throttler.recorder.WriteFrame(frame)
	}

	if throttler.throttledFrames == 0 {
		log.Printf("recording throttled")
		throttler.listener.WhenThrottled()
	}
	throttler.throttledFrames++
	return nil
}

// realClock implements ratelimit.Clock in terms of standard time functions.
type realClock struct{}

// Now implements Clock.Now by calling time.Now.
func (realClock) Now() time.Time {
	return time.Now()
}

// Now implements Clock.Sleep by calling time.Sleep.
func (realClock) Sleep(d time.Duration) {
	time.Sleep(d)
}