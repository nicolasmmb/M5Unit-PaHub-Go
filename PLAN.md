# PLAN — M5Unit-PaHub-Go (Pa.HUB v2.1)

Driver Go do **Unit PaHub v2.1** (SKU U040-B-V21, chip **PCA9548A**).
100% Go, sem C, sem CGo, sem Arduino.

- Módulo: `github.com/nicolasmmb/M5Unit-PaHub-Go`
- Diretório: `/Users/nicolas.mmb/__DEV__/M5Unit-PaHub-Go`
- Regra da família: `docs/LIB-CREATION-GUIDE.md` (inclui seção 0.1, agnóstico)
- Estado: implementação

## 1. Objetivo

1. Selecionar um dos 6 canais do hub por I2C.
2. Executar uma transação I2C já no canal escolhido, em uma chamada.
3. Ler de volta o registrador de controle (prova de presença do hub).
4. Varrer um canal e listar os endereços que respondem.
5. Núcleo sem `machine`. Construtor público fora de build tag.
6. Zero alocação por operação. Sem bloquear sem motivo.

## 2. Hardware

| Item | Valor |
|---|---|
| Produto | Unit PaHub v2.1 |
| SKU | U040-B-V21 |
| Chip | **PCA9548A** (multi-canal I2C switch) |
| Endereço | **0x70 a 0x77**, por DIP switch (A0, A1, A2) |
| Canais | 8 no chip. O unit expõe **6**. |
| Registrador | 1 byte. Bit N = canal N ligado. Reset = 0x00. |
| Clock | até 400 kHz |
| Conector | HY2.0-4P no PORT A (GND, 5 V, SDA, SCL) |

Fatos do datasheet que importam:

- O registrador de controle é **leitura e escrita**. Ler devolve a máscara.
  Isto serve como teste de presença: não existe registrador de ID.
- Depois do power-on, todos os canais ficam **desligados** (máscara 0).
- Escrever 0 desliga todos os canais.
- O hub **não** tem buffer. Ele liga o par SDA/SCL do canal escolhido ao barramento.
- Vários canais podem ficar ligados ao mesmo tempo. Isto permite aninhar dois hubs.

Nota de compatibilidade: o PaHub v1 (U040) usa TCA9548A. O registrador é igual.
A mesma lib serve nos dois.

## 3. Comportamento da lib original (C e UIFlow)

**Arduino (C)** — `Unit/PaHUB/Arduino/PaHUB/PaHUB.ino`:

```c
#define PAHUB_ADDR 0X70

void portselectall(uint8_t ports) {   // máscara de todos os canais
  Wire.beginTransmission(PAHUB_ADDR);
  Wire.write(ports & 0x3f);           // 6 bits
  Wire.endTransmission();
}

void portselect(uint8_t i) {          // um canal só
  Wire.beginTransmission(PAHUB_ADDR);
  Wire.write(1 << i);
  Wire.endTransmission();
}
```

**UIFlow (MicroPython)** — `unit/pahub.py`:

- `select_channel(ch)`: lê a máscara, aplica OR com `1<<ch`, escreve.
- `release_channel(ch)`: lê a máscara, aplica AND com `~(1<<ch)`, escreve.
- Proxy: `readfrom`, `writeto`, `readfrom_mem`, `writeto_mem`, `scan`, etc.
  Cada método seleciona o canal, executa, e libera o canal.
- `writeto` espera **100 ms** depois da escrita. É um contorno, não um requisito.
  Não vamos copiar.

## 4. Escopo

### 4.1 Dentro

- `Select(ch)`: escreve `1<<ch`. Igual ao `portselect` do C.
- `SelectAll(mask)`: escreve a máscara. Igual ao `portselectall` do C.
- `Enable(ch)` / `Disable(ch)`: OR e AND com a máscara atual, como a UIFlow.
- `Mask()`: lê o registrador. Detecta hub ausente.
- `Init()`: escreve 0 e confere a leitura.
- `Close()`: máscara 0.
- `Tx(ch, addr, w, r)`: seleciona, executa, **restaura** a máscara anterior.
- `TxContext(ctx, ...)`, `Scan(ch)`.
- Testes de host com fake PCA9548A (registrador + dispositivos atrás dos canais).
- Contrato de 0 alocação e footprint.
- Exemplo TinyGo e firmware de validação.

### 4.2 Fora

| Item | Motivo |
|---|---|
| Interrupção do PCA9548A (/INT) | O unit não expõe o pino de interrupção. |
| Alterar o endereço pelo DIP switch | É hardware. A lib só recebe o endereço. |
| Reset por pino | O unit não expõe o pino /RESET. |
| Copiar o `sleep(100ms)` da UIFlow | Contorno sem justificativa no datasheet. |
| Driver dos dispositivos atrás do hub | Cada device tem a sua lib. |

## 5. API

```go
type I2C interface {
    Tx(addr uint16, w, r []byte) error   // machine.I2C satisfaz
}

const (
    DefaultAddress = 0x70
    NumChannels    = 6
)

func New(bus I2C) *Device
func NewAt(bus I2C, addr uint16) (*Device, error) // 0x70..0x77
func (d *Device) Init() error                     // presença do hub
func (d *Device) Address() uint16

func (d *Device) Select(ch uint8) error     // 1<<ch
func (d *Device) SelectAll(mask uint8) error
func (d *Device) Enable(ch uint8) error     // OR
func (d *Device) Disable(ch uint8) error    // AND ~
func (d *Device) Mask() (uint8, error)
func (d *Device) Close() error              // SelectAll(0)

func (d *Device) Tx(ch uint8, addr uint16, w, r []byte) error
func (d *Device) TxContext(ctx context.Context, ch uint8, addr uint16, w, r []byte) error
func (d *Device) Scan(ch uint8) ([]uint8, error)
```

## 6. Decisões de projeto

1. **`Tx` restaura a máscara anterior.** A UIFlow apenas limpa o bit do canal.
   Restaurar é mais seguro: preserva canais ligados por outro código e permite
   aninhamento. Está documentado e testado.
2. **Tudo serializado por mutex.** A máscara é estado global do barramento. Duas
   goroutines em canais diferentes não podem intercalar.
3. **Sem `sleep`.** O `sleep(100ms)` da UIFlow fica fora.
4. **Sem registrador de ID.** `Init` e `Mask` usam o read-back do controle.
5. **`Scan` aloca** (devolve slice). É operação de diagnóstico, não de loop.
6. **Núcleo sem `machine`.** `machine.I2C` satisfaz `Tx(addr, w, r)`, igual a
   ToF90, NFC e Joystick2.

## 7. Testes

Fake do PCA9548A:

- Guarda a máscara em um byte.
- Recusa (NACK) quando o canal do dispositivo alvo está desligado.
- Vários dispositivos podem ter o mesmo endereço em canais diferentes.

Casos: presença/ausência, `Init` com hub mudo, `Select`, `SelectAll`,
`Enable`/`Disable` encadeados, `Mask`, `Close`, `Tx` com restauração, `Tx` em
canal desligado, `Scan` com e sem devices, endereço inválido, canal 6 e 7
(NumChannels), concorrência com `-race`, 0 alocação, footprint.

## 8. Fases

- [x] Pesquisa: SKU, chip, registrador, lib C, lib UIFlow.
- [ ] Núcleo `pahub.go` + erros.
- [ ] Testes com fake + contratos.
- [ ] `examples/scan` e `examples/validate`.
- [ ] README, BENCHMARKS, Makefile, CI (com job de agnosticismo).
- [ ] Validação em hardware com o unit.
- [ ] Publicar e tag `v0.1.0`.

## 9. Pendências conhecidas

- Validar em hardware: presença, troca de canal, dois devices no mesmo endereço.
- Medir a máscara real na saída do `Init` (esperado 0x00).
- Testar aninhamento com dois hubs (dois `Device`, dois endereços).
- CI de agnosticismo: compilar para `pico` e `arduino-nano33`.

## 10. Referências

- Doc v2.1: https://docs.m5stack.com/en/unit/Unit_PaHub_v2.1
- Doc v1 (TCA9548A): https://docs.m5stack.com/en/unit/Unit_PaHub
- Exemplo C: `M5-ProductExampleCodes/Unit/PaHUB/Arduino/PaHUB/PaHUB.ino`
- UIFlow: `uiflow-micropython/m5stack/libs/unit/pahub.py`
- Datasheet PCA9548A (NXP) e TCA9548A (TI)
