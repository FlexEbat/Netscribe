package render

import (
	"fmt"
	"math/rand"
	"testing"

	"github.com/FlexEbat/Netscribe/internal/model"
)

func dev(id int64, ip string, kind model.DeviceKind) model.Device {
	return model.Device{ID: id, IP: ip, Kind: kind}
}

func network() []model.Device {
	devs := []model.Device{dev(1, "192.168.1.1", model.KindRouter), dev(2, "192.168.1.2", model.KindSwitch)}
	for i := 0; i < 20; i++ {
		devs = append(devs, dev(int64(10+i), fmt.Sprintf("192.168.1.%d", 100+i), model.KindHost))
	}
	devs = append(devs, dev(50, "192.168.1.9", model.KindServer), dev(51, "", model.KindUnknown))
	return devs
}

func TestLayoutIsDeterministic(t *testing.T) {
	want := Layout(network())
	for seed := int64(0); seed < 20; seed++ {
		shuffled := network()
		rand.New(rand.NewSource(seed)).Shuffle(len(shuffled), func(i, j int) { shuffled[i], shuffled[j] = shuffled[j], shuffled[i] })
		byID := map[int64]model.Device{}
		for _, d := range Layout(shuffled) {
			byID[d.ID] = d
		}
		for _, w := range want {
			g := byID[w.ID]
			if *g.X != *w.X || *g.Y != *w.Y {
				t.Fatalf("seed %d: device %d at (%v,%v), want (%v,%v)", seed, w.ID, *g.X, *g.Y, *w.X, *w.Y)
			}
		}
	}
}

func TestLayoutNeverPlacesTwoDevicesOnTheSameSpot(t *testing.T) {
	seen := map[[2]float64]int64{}
	for _, d := range Layout(network()) {
		if d.X == nil || d.Y == nil {
			t.Fatalf("device %d has no position", d.ID)
		}
		p := [2]float64{*d.X, *d.Y}
		if other, dup := seen[p]; dup {
			t.Errorf("devices %d and %d share position %v", other, d.ID, p)
		}
		seen[p] = d.ID
	}
}

func TestLayoutKeepsSavedPositionsAndDoesNotChangeTheInput(t *testing.T) {
	x, y := 5.5, -7.0
	in := []model.Device{{ID: 1, IP: "10.0.0.1", Kind: model.KindRouter, X: &x, Y: &y}, dev(2, "10.0.0.2", model.KindHost)}
	out := Layout(in)
	if *out[0].X != 5.5 || *out[0].Y != -7 {
		t.Errorf("saved position moved to (%v,%v)", *out[0].X, *out[0].Y)
	}
	if in[1].X != nil || in[1].Y != nil {
		t.Error("Layout wrote into its input")
	}
}

func TestLayoutRowsFollowTheKindOrderAndWrapAfterEightNodes(t *testing.T) {
	var devs []model.Device
	for i := 0; i < 10; i++ {
		devs = append(devs, dev(int64(100+i), fmt.Sprintf("10.0.0.%d", 10+i), model.KindHost))
	}
	devs = append(devs, dev(1, "10.0.0.1", model.KindRouter), dev(2, "10.0.0.2", model.KindSwitch))
	pos := map[int64][2]float64{}
	for _, d := range Layout(devs) {
		pos[d.ID] = [2]float64{*d.X, *d.Y}
	}
	if pos[1] != [2]float64{0, 0} {
		t.Errorf("router at %v, want the first row", pos[1])
	}
	if pos[2] != [2]float64{0, RowStep} {
		t.Errorf("switch at %v, want the second row", pos[2])
	}
	if pos[100] != [2]float64{0, 2 * RowStep} || pos[107] != [2]float64{7 * ColumnStep, 2 * RowStep} {
		t.Errorf("first hosts at %v and %v", pos[100], pos[107])
	}
	if pos[108] != [2]float64{0, 3 * RowStep} || pos[109] != [2]float64{ColumnStep, 3 * RowStep} {
		t.Errorf("the ninth and tenth host at %v and %v, want a wrapped row", pos[108], pos[109])
	}
}

func TestLayoutOrdersByIPValueAndPutsMissingAddressesLast(t *testing.T) {
	devs := []model.Device{dev(1, "", model.KindHost), dev(2, "10.0.0.10", model.KindHost), dev(3, "10.0.0.9", model.KindHost)}
	order := map[int64]float64{}
	for _, d := range Layout(devs) {
		order[d.ID] = *d.X
	}
	if !(order[3] < order[2] && order[2] < order[1]) {
		t.Errorf("columns by id = %v, want 10.0.0.9, 10.0.0.10, no address", order)
	}
}

func TestLayoutTreatsAnUnexpectedKindAsUnknown(t *testing.T) {
	out := Layout([]model.Device{dev(1, "10.0.0.1", model.DeviceKind("toaster"))})
	if out[0].X == nil || out[0].Y == nil {
		t.Error("a device of an unexpected kind got no position")
	}
}

func TestLayoutOfNothing(t *testing.T) {
	if got := Layout(nil); len(got) != 0 {
		t.Errorf("Layout(nil) = %v", got)
	}
}
