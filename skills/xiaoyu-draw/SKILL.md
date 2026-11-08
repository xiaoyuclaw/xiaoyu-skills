---
name: xiaoyu-draw
description: 小鱼画图——用 ~/agents/xiaoyu/draw.py 通过智谱 CogView-3-Flash（免费默认）或 MiniMax / GLM-Image 生成图片，处理「AI生成」水印，并把图发给 QQ 用户。当用户要求画图、生图、配图、改图、对比画图效果，或询问画图价格与水印时使用。
---

# 小鱼画图

## 何时用这个 skill

用户说「画一张」「生成个图」「配张图」「帮我画」，或者要对比不同模型出图效果、问水印/价格时。

工具就一个：`~/agents/xiaoyu/draw.py`。不要临时手写 API 调用，也不要装新的 SDK。

## 默认走哪家

**默认智谱 CogView-3-Flash，免费（¥0）。** 不带参数就是它。

```bash
cd ~/agents/xiaoyu
./draw.py "银蓝长发少女，鲸鱼发饰，海边夕阳" --ratio 1:1
```

生成后会打印输出路径，默认落在 `assets/draw/<时间戳>.jpg`。要用 `--out 名字` 指定文件名（会自动加 `.jpg`）。

## 三家怎么选

| 需求 | 参数 | 单价 | 水印 |
|---|---|---|---|
| 默认、试构图、内部看 | 不加参数 | ¥0 | 有「AI生成」 |
| 要干净图、脸更细腻 | `--provider minimax` | ≈¥0.025 | 无 |
| 出成品、要最好画面 | `--model glm-image --allow-paid` | ¥0.1 | 有 |
| 免费备胎（质量不稳） | `--provider pollinations` | ¥0 | 有 |

智谱付费档还有 `cogview-4`（¥0.1），同价位不如 `glm-image`，一般不选。

**付费模型必须显式加 `--allow-paid`**，否则脚本直接拒绝——这是防止手滑烧钱，不要绕过它去手写请求。

## 水印怎么处理

智谱全系（含付费）右下角都有「AI生成」，MiniMax 没有。

**不要因为水印就自作主张换厂商。** 按情况：

- 底边没有关键内容 → 直接裁，不用问：
  ```bash
  ./draw.py "提示词" --crop-watermark        # 默认裁底部 8%
  ./draw.py "提示词" --crop-watermark 0.05   # 裁 5%，更保画面
  ```
  裁剪会降低画幅（1024×1024 → 1024×943），身体、脚、前景可能被切掉；
- 构图靠底，或者用户明确要原图 → 先问一句，或者换 `--provider minimax`（无水印）并说明费用；
- 对外发布时提醒一次：国内对 AI 生成内容有显式标识要求，裁水印属于绕过，风险由用户决定。

## 参数速查

```bash
--ratio 1:1|16:9|9:16|4:3|3:4      画幅比例
--n 2                              一次出多张（智谱/ MiniMax 都支持）
--model <名称>                     覆盖默认模型
--allow-paid                       允许智谱付费模型
--crop-watermark [比例]            裁掉底部去水印
--out <前缀>                       输出文件名
--timeout 秒                        默认 120
```

提示词：中文可用，但**英文描述构图和画风更稳**；角色一致性靠把外貌细节写全（发色、瞳色、服饰、配饰）。

## 凭据与失败排查

- key 位置：`credentials/zhipu.json`、`credentials/minimax.json`，都是 600，别打印、别写进日志。
- 智谱报 `1113 余额不足或无可用资源包` → 账户没钱了，改用免费档或 MiniMax。
- 智谱免费档报错 → 先确认模型名是 `cogview-3-flash`。
- Pollinations 报 `402` → 提示词里有被平台挡的词（例如 `envelope`），换词重试，或本来就是频率限制。
- MiniMax 报鉴权失败 → 检查 `credentials/minimax.json` 里的 key 是否完整。
- 出图是 JPEG 但保存成 `.png` 会导致读图工具报格式不符——按返回字节的实际格式命名。

## 把图发给用户

生成后按渠道发：

- QQ 当前会话：`qqbot_send_file`（工具，传绝对路径）；
- QQ 指定对象：`./qqbot.py image --to c2c:<openid> <路径>`；
- 邮件：`credentials/mail.py send`（附件支持需自行确认）。

发图前先自己用 `read_image` 看一眼，别把画歪的图直接甩给用户。
