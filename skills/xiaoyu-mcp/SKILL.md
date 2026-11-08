---
name: xiaoyu-mcp
description: 按需调用 MCP 服务器（高德地图、DeepWiki 等）——查地址、路线、周边、天气，或问 GitHub 仓库文档。工具不常驻工具列表，用时才调，省上下文。当用户问路线/距离/某地天气/附近有什么、要查 MCP、或说「接个 MCP 服务」时使用。
---

# 小鱼接 MCP（按需调用）

## 为什么不做成原生工具

原生挂进 profile 的 MCP 服务器，**每个工具的 schema 都常驻上下文**，用不用都在烧 token（高德 15 个工具 ≈ 上千 token/轮）。所以默认走「脚本 + 登记表」：零成本待命，用到才加载。

只有当某个服务器**每轮都要用**时，才值得写进 `cordis.patch.yml`（需要重启）。

## 已登记

| 名字 | 能力 | 鉴权 |
|---|---|---|
| `amap` | 高德：地理编码、逆地理、路径规划（驾车/步行/骑行/公交）、距离、POI 搜索、天气、IP 定位 | URL 带 key |
| `deepwiki` | GitHub 仓库文档问答 | 无需 |
| `train12306` | 12306 余票 / 票价 / 时刻 / 换乘 / 经停（`uvx mcp-server-12306`，本地 stdio） | 无需 |
| `tuniu` | 境内机票搜索（价位/余座）、酒店、门票（`uvx mcp-tuniu-travel`，本地 stdio） | 无需 |

登记表：`~/agents/xiaoyu/credentials/mcp_servers.json`（600）
客户端：`~/agents/xiaoyu/tools/mcp_call.py`

## 用法

```bash
cd ~/agents/xiaoyu
./tools/mcp_call.py list                  # 有哪些服务器
./tools/mcp_call.py tools amap            # 某服务器有哪些工具
./tools/mcp_call.py call amap maps_geo address=杭州市西湖区
./tools/mcp_call.py call amap maps_weather city=110000
./tools/mcp_call.py call amap maps_distance origins=116.48,39.99 destination=121.47,31.23
```

参数两种写法：`k=v` 或 `--json '{"k":"v"}'`（嵌套参数必须用后者）。

## 火车票（train12306）

```bash
./tools/mcp_call.py call train12306 query-tickets from_station=北京 to_station=上海 train_date=2026-11-12
./tools/mcp_call.py call train12306 query-ticket-price from_station=北京 to_station=上海 train_date=2026-11-12
./tools/mcp_call.py call train12306 search-stations query=虹桥
./tools/mcp_call.py call train12306 query-transfer from_station=北京 to_station=上海 train_date=2026-11-12
```

| 工具 | 用途 |
|---|---|
| `query-tickets` | 余票 + 车次 + 时刻（一次给全） |
| `query-ticket-price` | 各席别票价 |
| `search-stations` | 站名模糊匹配（支持拼音/三字码） |
| `query-transfer` | 中转换乘方案 |
| `get-train-route-stations` | 某车次经停站 |
| `get-train-no-by-train-code` | 车次号 → train_no |

要点：
- 站名用中文城市名即可（`北京`/`上海`），会自动匹配全部车站。
- 余票值 `有` / `无` / 数字（数字=剩余张数）。
- 首次运行 `uvx` 要下载包（约十几秒），之后走缓存。
- 服务若被 12306 风控，会返回 success:false 或空列表——先重试一次，仍失败就告诉用户去 App 查。

## 机票（tuniu）

```bash
./tools/mcp_call.py call tuniu flightSearch departureCityName=成都 arrivalCityName=北京 departureDate=2026-11-12
./tools/mcp_call.py call tuniu flightCabinDetail departureCityName=成都 arrivalCityName=北京 departureDate=2026-11-12 flightNo=MU6654
```

返回 `data.directFlights[]`，关键字段：

| 字段 | 含义 |
|---|---|
| `flightNumber` / `airlineCompany` | 航班号 / 航司 |
| `departureTime` / `arrivalTime` | 完整时间戳，取后 5 位就是钟点 |
| `departureAirport` / `arrivalAirport` | 机场（天府/双流/大兴…） |
| `totalDuration` | 如 `2h35m` |
| `basePrice` + `totalTax` | **含税价 = 两者相加** |
| `remainingSeats` | 余座，`9` 通常表示 9+ |

注意：
- **只读使用**。`flightSaveOrder` / `*CancelOrder` 是下单接口，不要碰。
- 价格是途牛的报价，可能有几分钟缓存；报给用户时说明"以下单页为准"。
- 城市名用中文（`成都`），不要用三字码。
- 只有境内航线数据好；国际航线覆盖差。

## 高德常用工具速查

| 工具 | 必填 | 返回 |
|---|---|---|
| `maps_geo` | address | location “经度,纬度” |
| `maps_regeocode` | location | 行政区划地址 |
| `maps_direction_driving` | origin, destination | distance(米)、duration(秒) |
| `maps_direction_transit_integrated` | origin, destination, city, cityd | 跨城公交/火车方案 |
| `maps_around_search` | keywords, location | 周边 POI |
| `maps_weather` | city | 当天+未来预报 |
| `maps_ip_location` | ip | IP 归属地 |

高德还有三个 `maps_schema_*`（生成专属地图、一键导航、一键打车），会把行程推到手机高德 App。

## 登记新服务器

编辑 `credentials/mcp_servers.json`，加一条即可，不用重启：

```json
{
  "名字": {"transport": "http", "url": "https://.../mcp", "headers": {"Authorization": "Bearer xxx"}},
  "本地": {"transport": "stdio", "command": "uv", "args": ["run", "/abs/path/server.py"], "env": {}}
}
```

- `名字` 会成为工具引用名，也是原生挂载时的前缀（`mcp__名字__工具`）
- `stdio` 的 `args` 用**绝对路径**，相对路径会因 cwd 变化失效
- 密钥直接放这个文件（600），别外传；也可以放 `credentials/` 下单独文件再让脚本读

## 坑

- **同名地点会返回多条**：`西湖区` 同时匹配杭州和南昌。查地点要带城市，或让用户确认。
- **单位**：距离是米、时长是秒，报给用户前换算。
- **跨城公交**必须给 `city` 和 `cityd`（起点城市、终点城市名）。
- key 在 URL 里，截图/日志要打码；泄露了去高德控制台重置。
- 服务器连不上只是报错，不会影响别的工具；超时默认 60 秒。

## 何时改走原生

某个服务器开始高频使用（每轮都要调）、或需要权限系统介入时，再写进 profile：

```yaml
- id: mcp-manager          # 注意是 mcp-manager，不是 mcp
  config:
    servers: {...}          # 整份替换，已有的要一起写
```
改完要重启 dsh web 才生效。
