// Command mkicon 生成「zterm」应用图标（飞牛风格：浅底玻璃质感 + 一处高饱和重点）。
//
// 造型：浅灰蓝渐变圆角方块 + 一块深色终端屏 + 屏内一枚红色提示符与光标，下面两行"输出"。
// 用符号距离场（SDF）逐像素渲染，3x 超采样后盒式降采样，不依赖任何第三方库。
// 用法：go run ./tools/mkicon -out deploy/fnos-app/zterm
package main

import (
	"flag"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"path/filepath"
)

const ss = 3 // 超采样倍数

// ---- 基础工具 ----

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

func lerp(a, b, t float64) float64 { return a + (b-a)*t }

// rgb 把 0xRRGGBB 转成 0..1 三元组。
func rgb(hex uint32) (float64, float64, float64) {
	return float64(hex>>16&0xff) / 255, float64(hex>>8&0xff) / 255, float64(hex&0xff) / 255
}

// ---- 形状（符号距离场，返回像素距离：负=内，正=外） ----

// 圆角盒：中心 (cx,cy)，半宽半高 (hx,hy)，圆角半径 r。
func sdRoundBox(x, y, cx, cy, hx, hy, r float64) float64 {
	qx := math.Abs(x-cx) - (hx - r)
	qy := math.Abs(y-cy) - (hy - r)
	ax := math.Max(qx, 0)
	ay := math.Max(qy, 0)
	return math.Hypot(ax, ay) + math.Min(math.Max(qx, qy), 0) - r
}

func sdCircle(x, y, cx, cy, r float64) float64 {
	return math.Hypot(x-cx, y-cy) - r
}

// 椭圆（近似距离，足够抗锯齿与柔影用）。
func sdEllipse(x, y, cx, cy, rx, ry float64) float64 {
	dx := (x - cx) / rx
	dy := (y - cy) / ry
	return (math.Hypot(dx, dy) - 1) * math.Min(rx, ry)
}

// 点到线段的距离：提示符的斜边用它加粗成"胶囊笔画"。
func sdSegment(x, y, ax, ay, bx, by float64) float64 {
	pax, pay := x-ax, y-ay
	bax, bay := bx-ax, by-ay
	h := clamp01((pax*bax+pay*bay)/(bax*bax+bay*bay))
	return math.Hypot(pax-bax*h, pay-bay*h)
}

func sdUnion(a, b float64) float64 { return math.Min(a, b) }

// ---- 图层 ----

// shade 返回某像素的非预乘颜色与不透明度（0..1）。
type shadeFunc func(x, y float64) (r, g, b, a float64)

type layer struct {
	sdf     func(x, y float64) float64
	feather float64 // 边缘过渡宽度（像素）：1≈锐利抗锯齿，越大越柔（用于投影/辉光）
	shade   shadeFunc
}

// solid 返回固定颜色（a 为不透明度）的 shader。
func solid(hex uint32, a float64) shadeFunc {
	r, g, b := rgb(hex)
	return func(x, y float64) (float64, float64, float64, float64) { return r, g, b, a }
}

// vgrad 返回垂直线性渐变（y0→y1，c0→c1，a0→a1）的 shader。
func vgrad(y0, y1 float64, c0, c1 uint32, a0, a1 float64) shadeFunc {
	r0, g0, b0 := rgb(c0)
	r1, g1, b1 := rgb(c1)
	return func(x, y float64) (float64, float64, float64, float64) {
		t := clamp01((y - y0) / (y1 - y0))
		return lerp(r0, r1, t), lerp(g0, g1, t), lerp(b0, b1, t), lerp(a0, a1, t)
	}
}

// render 按图层从后到前合成 size×size 的图标。
func render(size int, layers []layer) *image.RGBA {
	S := size * ss
	pix := make([]float64, S*S*4) // 预乘 RGBA，0..1
	for py := 0; py < S; py++ {
		fy := float64(py) + 0.5
		for px := 0; px < S; px++ {
			fx := float64(px) + 0.5
			i := (py*S + px) * 4
			for _, L := range layers {
				d := L.sdf(fx, fy)
				cov := clamp01(0.5 - d/L.feather)
				if cov <= 0 {
					continue
				}
				r, g, b, a := L.shade(fx, fy)
				a *= cov
				if a <= 0 {
					continue
				}
				ia := 1 - a
				pix[i] = r*a + pix[i]*ia
				pix[i+1] = g*a + pix[i+1]*ia
				pix[i+2] = b*a + pix[i+2]*ia
				pix[i+3] = a + pix[i+3]*ia
			}
		}
	}

	// 盒式降采样（预乘域平均）+ 反预乘
	out := image.NewRGBA(image.Rect(0, 0, size, size))
	area := float64(ss * ss)
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			var sr, sg, sb, sa float64
			for dy := 0; dy < ss; dy++ {
				for dx := 0; dx < ss; dx++ {
					j := ((y*ss+dy)*S + (x*ss + dx)) * 4
					sr += pix[j]
					sg += pix[j+1]
					sb += pix[j+2]
					sa += pix[j+3]
				}
			}
			r, g, b, a := sr/area, sg/area, sb/area, sa/area
			var R, G, B uint8
			if a > 0 {
				R = uint8(clamp01(r/a)*255 + 0.5)
				G = uint8(clamp01(g/a)*255 + 0.5)
				B = uint8(clamp01(b/a)*255 + 0.5)
			}
			out.SetRGBA(x, y, color.RGBA{R, G, B, uint8(clamp01(a)*255 + 0.5)})
		}
	}
	return out
}

// scene 返回 zterm 图标的全部图层（坐标为 0..1，渲染时乘 S）。
func scene(S float64) []layer {
	u := func(v float64) float64 { return v * S } // 归一化 → 像素

	// 底板：圆角方块，0.0156..0.984，圆角 0.2266（与飞牛官方图标同一套比例）
	tile := func(x, y float64) float64 {
		return sdRoundBox(x, y, u(0.5), u(0.5), u(0.484), u(0.484), u(0.2266))
	}
	// 终端屏：x 0.191..0.809，y 0.230..0.770，圆角 0.078
	screen := func(dy float64) func(x, y float64) float64 {
		return func(x, y float64) float64 {
			return sdRoundBox(x, y, u(0.5), u(0.5+dy), u(0.309), u(0.270), u(0.078))
		}
	}
	// 提示符 ">"：两条斜边拼成一个 V 形（朝右）
	chevron := func(x, y float64) float64 {
		r := u(0.030)
		a := sdSegment(x, y, u(0.325), u(0.418), u(0.398), u(0.489)) - r
		b := sdSegment(x, y, u(0.398), u(0.489), u(0.325), u(0.560)) - r
		return sdUnion(a, b)
	}
	// 光标块：提示符右边那个方块
	cursor := func(x, y float64) float64 {
		return sdRoundBox(x, y, u(0.468), u(0.489), u(0.052), u(0.052), u(0.016))
	}
	// 输出行（屏幕下半部的两行"文字"）
	out := func(cy, halfW, a float64) layer {
		return layer{
			sdf:     func(x, y float64) float64 { return sdRoundBox(x, y, u(0.3075+halfW), u(cy), u(halfW), u(0.0135), u(0.0135)) },
			feather: 1,
			shade:   solid(0xc3cad6, a),
		}
	}
	// 描边环（取底板距离的绝对值，得到一圈细边）
	ring := func(x, y float64) float64 {
		return math.Abs(tile(x, y)) - u(0.004)
	}

	return []layer{
		// 1 底板：浅灰蓝渐变（白底与深底都能看清）
		{sdf: tile, feather: 1, shade: vgrad(u(0.0156), u(0.984), 0xf6f8fb, 0xdae3ef, 1, 1)},
		// 2 内辉光：柔和冷光晕，让玻璃面有厚度
		{sdf: func(x, y float64) float64 { return sdEllipse(x, y, u(0.5), u(0.4375), u(0.4375), u(0.3437)) },
			feather: u(0.16), shade: solid(0xdce8fa, 0.55)},
		// 3 终端屏柔影（向下偏移、宽过渡）
		{sdf: screen(0.028), feather: u(0.06), shade: solid(0x2b3a55, 0.34)},
		// 4 终端屏本体：深色渐变（界面里唯一的深色重点）
		{sdf: screen(0), feather: 1, shade: vgrad(u(0.230), u(0.770), 0x2a2e36, 0x16181d, 1, 1)},
		// 5 屏内顶部高光：模拟玻璃反光
		{sdf: func(x, y float64) float64 {
			return sdRoundBox(x, y, u(0.5), u(0.284), u(0.309), u(0.054), u(0.070))
		}, feather: 1, shade: solid(0xffffff, 0.09)},
		// 6 提示符：全图唯一的高饱和色（比主色亮一档，深底上够亮）
		{sdf: chevron, feather: 1, shade: solid(0xe04b5e, 1)},
		// 7 光标块
		{sdf: cursor, feather: 1, shade: solid(0xf2f3f5, 0.94)},
		// 8 两行输出
		out(0.623, 0.121, 0.55),
		out(0.694, 0.086, 0.34),
		// 9 屏边框（极淡，压住深色块的边）
		{sdf: func(x, y float64) float64 { return math.Abs(screen(0)(x, y)) - u(0.004) },
			feather: 1, shade: solid(0x0f1115, 0.28)},
		// 10 底板描边（白底上勾出轮廓）
		{sdf: ring, feather: 1, shade: solid(0xb0344a, 0.11)},
	}
}

func writePNG(path string, img image.Image) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	enc := png.Encoder{CompressionLevel: png.BestCompression}
	return enc.Encode(f, img)
}

func main() {
	out := flag.String("out", "deploy/fnos-app/zterm", "fpk 包目录")
	flag.Parse()

	targets := []struct {
		size int
		name string
	}{
		{64, "ICON.PNG"},
		{256, "ICON_256.PNG"},
		{64, "app/ui/images/icon_64.png"},
		{256, "app/ui/images/icon_256.png"},
	}
	cache := map[int]image.Image{}
	for _, t := range targets {
		img, ok := cache[t.size]
		if !ok {
			img = render(t.size, scene(float64(t.size*ss)))
			cache[t.size] = img
		}
		path := filepath.Join(*out, filepath.FromSlash(t.name))
		if err := writePNG(path, img); err != nil {
			panic(err)
		}
		info, _ := os.Stat(path)
		println("写入", path, info.Size(), "字节")
	}
}
