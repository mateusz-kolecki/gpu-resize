// Package opencl contains the OpenCL C kernels used for GPU image resizing.
// Kernels are embedded as string constants so the binary is self-contained
// with no external .cl files required.
package opencl

// KernelBilinear is an OpenCL C kernel that performs bilinear interpolation.
// It reads from a flat RGBA uint8 input buffer and writes to an output buffer
// of the same layout.
//
// Arguments:
//
//	__global uchar *src    - input image (RGBA, row-major)
//	__global uchar *dst    - output image (RGBA, row-major)
//	int src_w, src_h       - source dimensions
//	int dst_w, dst_h       - destination dimensions
const KernelBilinear = `
__kernel void resize_bilinear(
    __global const uchar4 *src,
    __global uchar4       *dst,
    int src_w, int src_h,
    int dst_w, int dst_h)
{
    int x = get_global_id(0);
    int y = get_global_id(1);
    if (x >= dst_w || y >= dst_h) return;

    float scaleX = (float)(src_w) / (float)(dst_w);
    float scaleY = (float)(src_h) / (float)(dst_h);

    float srcX = ((float)x + 0.5f) * scaleX - 0.5f;
    float srcY = ((float)y + 0.5f) * scaleY - 0.5f;

    int x0 = (int)floor(srcX);
    int y0 = (int)floor(srcY);
    int x1 = x0 + 1;
    int y1 = y0 + 1;

    x0 = clamp(x0, 0, src_w - 1);
    x1 = clamp(x1, 0, src_w - 1);
    y0 = clamp(y0, 0, src_h - 1);
    y1 = clamp(y1, 0, src_h - 1);

    float fx = srcX - floor(srcX);
    float fy = srcY - floor(srcY);

    uchar4 p00 = src[y0 * src_w + x0];
    uchar4 p10 = src[y0 * src_w + x1];
    uchar4 p01 = src[y1 * src_w + x0];
    uchar4 p11 = src[y1 * src_w + x1];

    float4 f00 = convert_float4(p00);
    float4 f10 = convert_float4(p10);
    float4 f01 = convert_float4(p01);
    float4 f11 = convert_float4(p11);

    float4 top    = mix(f00, f10, fx);
    float4 bottom = mix(f01, f11, fx);
    float4 result = mix(top, bottom, fy);

    dst[y * dst_w + x] = convert_uchar4_sat(result);
}
`

// KernelLanczos is an OpenCL C kernel implementing a separable Lanczos-3
// filter. Two passes are required: first horizontal (pass=0), then
// vertical (pass=1), writing to an intermediate buffer between passes.
//
// Arguments:
//
//	__global uchar4  *src  - source image (RGBA uint8, row-major)
//	__global float4  *tmp  - intermediate float buffer (same dimensions as dst for horiz pass,
//	                         same as src height × dst width for vert pass)
//	__global uchar4  *dst  - final output (RGBA uint8, row-major)
//	int src_w, src_h       - source dimensions
//	int dst_w, dst_h       - target dimensions
//	int pass               - 0 = horizontal src→tmp, 1 = vertical tmp→dst
const KernelLanczos = `
#define LANCZOS_A 3.0f

float lanczos_weight(float x) {
    if (x == 0.0f) return 1.0f;
    if (x >= LANCZOS_A || x <= -LANCZOS_A) return 0.0f;
    float px = M_PI_F * x;
    return LANCZOS_A * sin(px) * sin(px / LANCZOS_A) / (px * px);
}

/* Horizontal pass: src (uchar4) -> tmp (float4) */
__kernel void lanczos_horizontal(
    __global const uchar4 *src,
    __global float4       *tmp,
    int src_w, int src_h,
    int dst_w)
{
    int dx = get_global_id(0);
    int y  = get_global_id(1);
    if (dx >= dst_w || y >= src_h) return;

    float scale = (float)src_w / (float)dst_w;
    float center = ((float)dx + 0.5f) * scale - 0.5f;
    int start = (int)floor(center - LANCZOS_A) + 1;
    int end   = (int)floor(center + LANCZOS_A);

    float4 acc = (float4)(0.0f);
    float  wsum = 0.0f;

    for (int sx = start; sx <= end; sx++) {
        int csx = clamp(sx, 0, src_w - 1);
        float w = lanczos_weight(center - (float)sx);
        acc  += w * convert_float4(src[y * src_w + csx]);
        wsum += w;
    }

    tmp[y * dst_w + dx] = (wsum > 0.0f) ? (acc / wsum) : (float4)(0.0f);
}

/* Vertical pass: tmp (float4, src_h rows × dst_w cols) -> dst (uchar4) */
__kernel void lanczos_vertical(
    __global const float4 *tmp,
    __global uchar4       *dst,
    int src_h, int dst_w, int dst_h)
{
    int dx = get_global_id(0);
    int dy = get_global_id(1);
    if (dx >= dst_w || dy >= dst_h) return;

    float scale  = (float)src_h / (float)dst_h;
    float center = ((float)dy + 0.5f) * scale - 0.5f;
    int start = (int)floor(center - LANCZOS_A) + 1;
    int end   = (int)floor(center + LANCZOS_A);

    float4 acc  = (float4)(0.0f);
    float  wsum = 0.0f;

    for (int sy = start; sy <= end; sy++) {
        int csy = clamp(sy, 0, src_h - 1);
        float w = lanczos_weight(center - (float)sy);
        acc  += w * tmp[csy * dst_w + dx];
        wsum += w;
    }

    float4 result = (wsum > 0.0f) ? (acc / wsum) : (float4)(0.0f);
    dst[dy * dst_w + dx] = convert_uchar4_sat(result);
}
`
