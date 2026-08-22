package matchmake_extension

import (
	"testing"

	"github.com/PretendoNetwork/nex-go/v2/types"
)

func TestCanonicalWiiSportsClubGameMode(t *testing.T) {
	tests := []struct {
		name string
		in   uint32
		want uint32
	}{
		{name: "USA 100-pin", in: 0x03022400, want: 0x03022000},
		{name: "EUR 100-pin", in: 0x03022800, want: 0x03022000},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := canonicalWiiSportsClubGameMode(types.NewUInt32(tt.in))
			if uint32(got) != tt.want {
				t.Fatalf("got %#08x, want %#08x", uint32(got), tt.want)
			}
		})
	}
}
