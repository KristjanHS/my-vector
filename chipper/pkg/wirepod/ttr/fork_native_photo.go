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
var forkNativePhotoWait, forkNativePhotoEvery = 30 * time.Second, 500 * time.Millisecond

// forkNativePhoto keeps the photo intent_photo_take_extend makes on esn: newest id read before the robot shoots, then
// the first newer id fetched in full and stored fresh, so F3/F4 follow-ups reuse it (2026-11-10-native-photo-followup.md
// (docs/plans/) N1-N3). A failed fetch retries on the next poll; no photo in time → logged, store untouched.
func forkNativePhoto(esn, l string) {
	robot, err := forkPhotoRobot(esn)
	if err != nil {
		logger.Println("myvector native photo: " + err.Error())
		return
	}
	end := time.Now().Add(forkNativePhotoWait) // wall clock: tests freeze forkNow
	ctx, cancel := context.WithDeadline(context.Background(), end)
	defer cancel()
	before, had, ok := forkNewestPhoto(ctx, robot)
	if !ok {
		return
	}
	for time.Now().Before(end) {
		time.Sleep(forkNativePhotoEvery)
		id, has, ok := forkNewestPhoto(ctx, robot)
		if !ok || !has || (had && id <= before) {
			continue
		}
		resp, err := robot.Conn.Photo(ctx, &vectorpb.PhotoRequest{PhotoId: id})
		if err != nil || resp == nil || !resp.Success || len(resp.Image) == 0 {
			logger.Println("myvector native photo: fetch " + strconv.Itoa(int(id)) + " failed")
			continue
		}
		forkPhotos.PutFresh(esn, resp.Image, l, forkNow())
		logger.Println("myvector native photo: kept " + strconv.Itoa(int(id)))
		return
	}
	logger.Println("myvector native photo: none within " + forkNativePhotoWait.String())
}

// forkNewestPhoto is the robot's highest photo id and whether it has any; ok false when PhotosInfo fails.
func forkNewestPhoto(ctx context.Context, robot *vector.Vector) (max uint32, has, ok bool) {
	resp, err := robot.Conn.PhotosInfo(ctx, &vectorpb.PhotosInfoRequest{})
	if err != nil || resp == nil {
		return 0, false, false
	}
	for _, p := range resp.PhotoInfos {
		if id := p.GetPhotoId(); !has || id > max {
			max, has = id, true
		}
	}
	return max, has, true
}
