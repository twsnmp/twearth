package collector

import (
	"math"
	"sort"
	"time"
)

// BuildSynopticData computes 4-hPa isobar contour lines and High/Low pressure centers from a regular grid.
func BuildSynopticData(points []WindGridPoint) SynopticData {
	res := SynopticData{
		Timestamp: time.Now().UTC(),
	}
	if len(points) < 4 {
		return res
	}

	latSet := make(map[float64]struct{})
	lonSet := make(map[float64]struct{})
	for _, p := range points {
		if p.PressureMSL <= 0 {
			continue
		}
		latSet[roundCoord(p.Latitude)] = struct{}{}
		lonSet[roundCoord(p.Longitude)] = struct{}{}
	}

	if len(latSet) < 2 || len(lonSet) < 2 {
		return res
	}

	lats := make([]float64, 0, len(latSet))
	for lat := range latSet {
		lats = append(lats, lat)
	}
	sort.Float64s(lats)

	lons := make([]float64, 0, len(lonSet))
	for lon := range lonSet {
		lons = append(lons, lon)
	}
	sort.Float64s(lons)

	rows := len(lats)
	cols := len(lons)
	grid := make([][]float64, rows)
	valid := make([][]bool, rows)
	for r := 0; r < rows; r++ {
		grid[r] = make([]float64, cols)
		valid[r] = make([]bool, cols)
	}

	latIdx := make(map[float64]int, rows)
	for i, lat := range lats {
		latIdx[lat] = i
	}
	lonIdx := make(map[float64]int, cols)
	for j, lon := range lons {
		lonIdx[lon] = j
	}

	minP := math.MaxFloat64
	maxP := -math.MaxFloat64
	for _, p := range points {
		if p.PressureMSL <= 0 {
			continue
		}
		r, okR := latIdx[roundCoord(p.Latitude)]
		c, okC := lonIdx[roundCoord(p.Longitude)]
		if !okR || !okC {
			continue
		}
		grid[r][c] = p.PressureMSL
		valid[r][c] = true
		if p.PressureMSL < minP {
			minP = p.PressureMSL
		}
		if p.PressureMSL > maxP {
			maxP = p.PressureMSL
		}
	}

	if minP == math.MaxFloat64 || maxP == -math.MaxFloat64 {
		return res
	}

	// Generate 4 hPa contour levels covering [minP, maxP].
	startLevel := math.Ceil(minP/4.0) * 4.0
	endLevel := math.Floor(maxP/4.0) * 4.0

	for level := startLevel; level <= endLevel; level += 4.0 {
		segs := marchingSquaresSegments(lats, lons, grid, valid, level)
		if len(segs) == 0 {
			continue
		}
		polylines := stitchSegments(segs)
		isMajor := math.Mod(math.Round(level), 20) == 0
		for _, line := range polylines {
			if len(line) < 2 {
				continue
			}
			res.Isobars = append(res.Isobars, IsobarLine{
				Pressure:    level,
				IsMajor:     isMajor,
				Coordinates: line,
			})
		}
	}

	// Detect local High (H) and Low (L) pressure centers in the interior of the grid.
	for r := 1; r < rows-1; r++ {
		for c := 1; c < cols-1; c++ {
			if !valid[r][c] {
				continue
			}
			val := grid[r][c]
			isHigh := true
			isLow := true
			allNeighborsValid := true
			for dr := -1; dr <= 1; dr++ {
				for dc := -1; dc <= 1; dc++ {
					if dr == 0 && dc == 0 {
						continue
					}
					if !valid[r+dr][c+dc] {
						allNeighborsValid = false
						break
					}
					nb := grid[r+dr][c+dc]
					if nb >= val {
						isHigh = false
					}
					if nb <= val {
						isLow = false
					}
				}
				if !allNeighborsValid {
					break
				}
			}
			if !allNeighborsValid {
				continue
			}
			if isHigh && val >= 1014.0 {
				res.Centers = append(res.Centers, PressureCenter{
					Kind:      "H",
					Pressure:  math.Round(val*10) / 10,
					Latitude:  lats[r],
					Longitude: lons[c],
				})
			} else if isLow && val <= 1010.0 {
				res.Centers = append(res.Centers, PressureCenter{
					Kind:      "L",
					Pressure:  math.Round(val*10) / 10,
					Latitude:  lats[r],
					Longitude: lons[c],
				})
			}
		}
	}

	return res
}

func roundCoord(v float64) float64 {
	return math.Round(v*100) / 100
}

type msSegment struct {
	a [2]float64 // [lon, lat]
	b [2]float64
}

func marchingSquaresSegments(lats, lons []float64, grid [][]float64, valid [][]bool, level float64) []msSegment {
	rows := len(lats)
	cols := len(lons)
	var segs []msSegment

	interp := func(p1, p2, v1, v2 float64) float64 {
		dv := v2 - v1
		if math.Abs(dv) < 1e-6 {
			return (p1 + p2) * 0.5
		}
		t := (level - v1) / dv
		if t < 0 {
			t = 0
		} else if t > 1 {
			t = 1
		}
		return p1 + t*(p2-p1)
	}

	for r := 0; r < rows-1; r++ {
		for c := 0; c < cols-1; c++ {
			if !valid[r][c] || !valid[r][c+1] || !valid[r+1][c+1] || !valid[r+1][c] {
				continue
			}
			// Corners: bl(r,c), br(r,c+1), tr(r+1,c+1), tl(r+1,c)
			vBL := grid[r][c]
			vBR := grid[r][c+1]
			vTR := grid[r+1][c+1]
			vTL := grid[r+1][c]

			latB, latT := lats[r], lats[r+1]
			lonL, lonR := lons[c], lons[c+1]

			var mask int
			if vBL >= level {
				mask |= 1
			}
			if vBR >= level {
				mask |= 2
			}
			if vTR >= level {
				mask |= 4
			}
			if vTL >= level {
				mask |= 8
			}
			if mask == 0 || mask == 15 {
				continue
			}

			// Edge midpoints:
			// 0: bottom (BL -> BR), 1: right (BR -> TR), 2: top (TL -> TR), 3: left (BL -> TL)
			edgePt := func(edge int) [2]float64 {
				switch edge {
				case 0:
					return [2]float64{interp(lonL, lonR, vBL, vBR), latB}
				case 1:
					return [2]float64{lonR, interp(latB, latT, vBR, vTR)}
				case 2:
					return [2]float64{interp(lonL, lonR, vTL, vTR), latT}
				default:
					return [2]float64{lonL, interp(latB, latT, vBL, vTL)}
				}
			}

			addPair := func(e1, e2 int) {
				segs = append(segs, msSegment{a: edgePt(e1), b: edgePt(e2)})
			}

			switch mask {
			case 1, 14:
				addPair(3, 0)
			case 2, 13:
				addPair(0, 1)
			case 3, 12:
				addPair(3, 1)
			case 4, 11:
				addPair(1, 2)
			case 5:
				avg := (vBL + vBR + vTR + vTL) * 0.25
				if avg >= level {
					addPair(3, 2)
					addPair(0, 1)
				} else {
					addPair(3, 0)
					addPair(1, 2)
				}
			case 6, 9:
				addPair(0, 2)
			case 7, 8:
				addPair(3, 2)
			case 10:
				avg := (vBL + vBR + vTR + vTL) * 0.25
				if avg >= level {
					addPair(3, 0)
					addPair(1, 2)
				} else {
					addPair(3, 2)
					addPair(0, 1)
				}
			}
		}
	}
	return segs
}

func stitchSegments(segs []msSegment) [][][]float64 {
	type ptKey struct {
		x int64
		y int64
	}
	makeKey := func(p [2]float64) ptKey {
		return ptKey{
			x: int64(math.Round(p[0] * 10000)),
			y: int64(math.Round(p[1] * 10000)),
		}
	}

	adj := make(map[ptKey][]int, len(segs)*2)
	for i, s := range segs {
		ka := makeKey(s.a)
		kb := makeKey(s.b)
		adj[ka] = append(adj[ka], i)
		adj[kb] = append(adj[kb], i)
	}

	used := make([]bool, len(segs))
	var lines [][][]float64

	for i := range segs {
		if used[i] {
			continue
		}
		used[i] = true
		chain := [][2]float64{segs[i].a, segs[i].b}

		// Extend forward from chain[len-1]
		for {
			tail := makeKey(chain[len(chain)-1])
			nextIdx := -1
			for _, idx := range adj[tail] {
				if !used[idx] {
					nextIdx = idx
					break
				}
			}
			if nextIdx == -1 {
				break
			}
			used[nextIdx] = true
			s := segs[nextIdx]
			if makeKey(s.a) == tail {
				chain = append(chain, s.b)
			} else {
				chain = append(chain, s.a)
			}
		}

		// Extend backward from chain[0]
		for {
			head := makeKey(chain[0])
			prevIdx := -1
			for _, idx := range adj[head] {
				if !used[idx] {
					prevIdx = idx
					break
				}
			}
			if prevIdx == -1 {
				break
			}
			used[prevIdx] = true
			s := segs[prevIdx]
			var pt [2]float64
			if makeKey(s.a) == head {
				pt = s.b
			} else {
				pt = s.a
			}
			chain = append([][2]float64{pt}, chain...)
		}

		out := make([][]float64, len(chain))
		for j, p := range chain {
			out[j] = []float64{math.Round(p[0]*1000) / 1000, math.Round(p[1]*1000) / 1000}
		}
		lines = append(lines, out)
	}
	return lines
}
