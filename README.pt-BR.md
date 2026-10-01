# go-image

**[Read this in English / Leia em inglês](README.md)**

Filtros de imagem simples em Go, escritos para praticar concorrência e
profiling. O programa decodifica `download.jpeg`, zera o canal verde de cada
pixel em paralelo (uma goroutine por CPU, cada uma trabalhando em uma faixa
horizontal de linhas) e grava o resultado em `output_no.jpg`.

Usa só a biblioteca padrão (`image`, `image/jpeg`, `image/png`, `image/draw`).

## Filtros

| Função | O que faz | Alocações |
|---|---|---|
| `removeComponentFilter` | Zera o canal verde, no próprio buffer, em uma faixa de linhas | nenhuma |
| `applyGrayscaleFilterInPlace` | Substitui R, G e B pela média deles, no próprio buffer | nenhuma |
| `BlurInto` | Box blur separável com janela deslizante, escrito em `dst` usando um buffer auxiliar passado por quem chama | nenhuma |
| `applyBlurFilter` | Forma de conveniência do blur, que aloca o resultado e o buffer auxiliar | dois buffers |
| `distributePixels` | Divide a imagem em `n` faixas horizontais que cobrem cada pixel exatamente uma vez | um slice |

Todos os filtros acessam `image.RGBA.Pix` diretamente em vez de passar por
`At()` → `Convert()` → `Set()`, que encaixotava uma cor no heap duas ou três
vezes por pixel.

O blur é o único filtro que não funciona no próprio buffer: cada pixel de saída
é a média de uma vizinhança, então escrever por cima da origem corromperia a
entrada dos pixels seguintes. Como ele é separável, o custo por pixel não
depende do raio. As bordas são tratadas por clamp (o pixel da borda é repetido).

## Defeitos corrigidos

O profiling da primeira versão revelou três bugs, cada um agora coberto por um
teste:

- **Saída preta:** o filtro recebia só o destino recém-alocado (todo zerado),
  nunca a origem. O destino agora começa como uma cópia da origem.
- **Pixels perdidos:** a divisão montava os cantos dos retângulos a partir de
  um índice linear de pixel; com 4 goroutines, 49,6% da imagem nunca era
  visitada, com 8, 74,5%. Agora a divisão é por linhas.
- **Overflow nos tons de cinza:** a soma de três `uint8` dava a volta em 256 antes
  da divisão, e o branco virava cinza 84. A soma agora é feita em `int`.

## Executando

Requer Go 1.21+.

```sh
go run .            # lê download.jpeg, grava output_no.jpg
```

## Testes e benchmarks

```sh
go test ./...
go test -bench=. -benchmem
```

`TestFiltersDoNotAllocate` usa `testing.AllocsPerRun` para que uma alocação não
volte aos laços por pixel sem ninguém perceber.

Benchmarks sobre `download.jpeg` (274×184), antes e depois de remover a alocação
por pixel:

| Benchmark | Antes | Depois | Variação |
|---|---|---|---|
| `removeComponentFilter` | 1.391.686 ns/op | 46.802 ns/op | −96,6% |
| `applyGrayscaleFilter` | 2.026.270 ns/op | 276.094 ns/op | −86,4% |
| `Pipeline8` | 459.728 ns/op | 45.334 ns/op | −90,1% |

Blur separável vs. a forma ingênua O(r²):

| Raio | Separável | Ingênua | Ganho |
|---|---|---|---|
| 1 | 536 µs | 1,1 ms | 2× |
| 4 | 383 µs | 9,4 ms | 24× |
| 16 | 406 µs | 119,4 ms | 294× |
