# 生活服务搜索

## 大众点评

```bash
python fetch.py "https://www.dianping.com/search/keyword/0/0_关键词" -e playwright -w 5
```

## 美团

```bash
python fetch.py "https://i.meituan.com/" -i -e playwright
```

## 携程

```bash
python fetch.py "https://so.ctrip.com/hotels/?keyword=关键词" -e playwright -w 5
```

## 12306

```bash
python fetch.py "https://kyfw.12306.cn/otn/leftTicket/init" -i -e playwright
```

## 高德地图

```bash
python fetch.py "https://www.amap.com/search?query=关键词" -e playwright -w 5
```

## 百度地图

```bash
python fetch.py "https://map.baidu.com/search/关键词" -e playwright -w 5
```

## 去哪儿

```bash
python fetch.py "https://www.qunar.com/" -e playwright -w 5
```
