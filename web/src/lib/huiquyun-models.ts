// 汇取云模型名判定的唯一事实来源：能力默认值、协议识别和视频请求分发都读这里，
// 避免同一套模型名规则在 lib、services 和页面各抄一份后互相漂移。

const HUIQUYUN_VIDEO_MARKERS = ["mj-sd", "seedance", "grok-video", "sora", "veo", "kling", "hailuo", "vidu", "wan-video", "jimeng-video", "doubao-video", "minimax-video", "video"];

export function isHuiQuYunMX933Model(model: string) {
    const normalized = model.trim().toLowerCase();
    return normalized.startsWith("sd2-mx933-720-") || normalized.startsWith("sd2-mx933-720-fast-");
}

export function huiQuYunFixedVideoDuration(model: string) {
    const matched = model.trim().toLowerCase().match(/-(5|10|15)s$/);
    return matched ? Number(matched[1]) : 0;
}

// 汇取云视频模型名不一定带 video 字样：MX933 家族和固定时长后缀都是确定的视频合同，
// 必须先于关键字兜底判定，否则会落到文本协议并让上游返回无效请求。
export function isHuiQuYunVideoModel(model: string) {
    const normalized = model.trim().toLowerCase();
    if (isHuiQuYunMX933Model(normalized) || huiQuYunFixedVideoDuration(normalized) > 0) return true;
    return HUIQUYUN_VIDEO_MARKERS.some((marker) => normalized.includes(marker));
}
