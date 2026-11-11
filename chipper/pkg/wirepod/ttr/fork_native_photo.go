package wirepod_ttr

import (
	"context"
	"strconv"
	"time"

	"github.com/fforchino/vector-go-sdk/pkg/vector"
	"github.com/fforchino/vector-go-sdk/pkg/vectorpb"
	"github.com/kercre123/wire-pod/chipper/pkg/logger"
)

// The robot's own photo shows up in PhotosInfo within forkNativePhotoWait, polled every forkNativePhotoEvery (N1).
var forkNativePhotoWait, forkNativePhotoEvery = 15 * time.Second, 500 * time.Millisecond

// forkNativePhoto keeps the photo intent_photo_take_extend makes on esn: newest id read before the robot shoots, then
// the first newer id fetched in full and stored answered, so F3/F4 follow-ups reuse it (2026-11-10-native-photo-followup.md
// (docs/plans/) N1, N2). No new photo in time → logged, store untouched.
func forkNativePhoto(esn, l string) {
	robot, err := forkPhotoRobot(esn)
	if err != nil {
		logger.Println("myvector native photo: " + err.Error())
		return
	}
	before, ok := forkNewestPhoto(robot)
	if !ok {
		return
	}
	for end := time.Now().Add(forkNativePhotoWait); time.Now().Before(end); { // wall clock: tests freeze forkNow
		time.Sleep(forkNativePhotoEvery)
		id, ok := forkNewestPhoto(robot)
		if !ok || id <= before {
			continue
		}
		resp, err := robot.Conn.Photo(context.Background(), &vectorpb.PhotoRequest{PhotoId: id})
		if err != nil || resp == nil || len(resp.Image) == 0 {
			logger.Println("myvector native photo: fetch " + strconv.Itoa(int(id)) + " failed")
			return
		}
		now := forkNow()
		forkPhotos.Put(esn, resp.Image, l, now)
		forkPhotos.Answered(esn, now)
		logger.Println("myvector native photo: kept " + strconv.Itoa(int(id)))
		return
	}
	logger.Println("myvector native photo: none within " + forkNativePhotoWait.String())
}

// forkNewestPhoto is the robot's highest photo id (0 with none); false when PhotosInfo fails.
func forkNewestPhoto(robot *vector.Vector) (uint32, bool) {
	resp, err := robot.Conn.PhotosInfo(context.Background(), &vectorpb.PhotosInfoRequest{})
	if err != nil || resp == nil {
		return 0, false
	}
	var max uint32
	for _, p := range resp.PhotoInfos {
		if p.GetPhotoId() > max {
			max = p.GetPhotoId()
		}
	}
	return max, true
}
