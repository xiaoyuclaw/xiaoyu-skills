# 学术搜索

## Arxiv（AI/物理/数学论文）

```bash
python fetch.py "https://arxiv.org/search/?query=关键词" -e cffi
```

## PapersWithCode（论文+代码）

```bash
python fetch.py "https://paperswithcode.com/search?q=关键词" -e cffi
```

## Semantic Scholar（语义搜索）

```bash
python fetch.py "https://www.semanticscholar.org/search?q=关键词" -e playwright -w 5
```

## PubMed（医学文献）

```bash
python fetch.py "https://pubmed.ncbi.nlm.nih.gov/?term=关键词" -e cffi
```

## 知网（CNKI）

```bash
python fetch.py "https://kns.cnki.net/kns8s/defaultresult/index?kw=关键词" -e playwright -w 5
```

## Google Scholar

```bash
python fetch.py "https://scholar.google.com/scholar?q=关键词" -e playwright -w 5
```
