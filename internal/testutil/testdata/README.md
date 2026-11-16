# 测试图片

- `images/hdr.jxr` 是 64 × 64 的红蓝渐变，在线性 RGB 下将亮度放大至 4 倍，用于验证 JXR 转换、HDR 增益图和 SDR 缩略图。
- `images/sdr.jxr` 是相同尺寸的红蓝渐变，使用常规 SDR 亮度，用于验证单独标注 SDR HEIC。
- `images/solid.heic` 是 600 × 300 的纯色图片，用于验证普通位图预览和原件下载。

样本均由项目生成。进入本目录后可用以下命令重建。

```sh
magick -size 64x64 gradient:red-blue -colorspace RGB -evaluate Multiply 4 -alpha on -type TrueColorAlpha -define quantum:format=floating-point -depth 32 color.tif
JxrEncApp -i color.tif -o images/hdr.jxr -c 28
magick -size 64x64 gradient:red-blue -colorspace RGB -alpha on -type TrueColorAlpha -define quantum:format=floating-point -depth 32 sdr.tif
JxrEncApp -i sdr.tif -o images/sdr.jxr -c 28
magick -size 600x300 xc:steelblue -strip -quality 75 images/solid.heic
```

Go 测试通过 `testutil.ReadImage` 读取样本。
