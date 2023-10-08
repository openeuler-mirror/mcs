
#include <linux/types.h>
#include <linux/pci.h>
#include <linux/delay.h>
#include <linux/pgtable.h>
#include <linux/dma-map-ops.h>
#include "i210_eth.h"
#include "i210_intel.h"

extern void set_bar_addr(unsigned long phy_addr);

/* 物理地址基址 */
u64 g_rxRingBase;
u64 g_rxDmaBase;
u64 g_txRingBase;
u64 g_txDmaBase;
u64 g_macDevBase;

/* (LINUX侧视角)虚拟地址基址 */
u64 g_rxRingHostVirtBase;
u64 g_rxDmaHostVirtBase;
u64 g_txRingHostVirtBase;
u64 g_txDmaHostVirtBase;
u64 g_macDevHostVirtBase;

/* (RTOS侧视角)虚拟地址基址 */
u64 g_rxRingVirtBase;
u64 g_rxDmaVirtBase;
u64 g_txRingVirtBase;
u64 g_txDmaVirtBase;
u64 g_macDevVirtBase;

/* (设备侧视角)总线地址基址 */
u64 g_rxRingBusBase;
u64 g_rxDmaBusBase;
u64 g_txRingBusBase;
u64 g_txDmaBusBase;
u64 g_macDevBusBase;

int bus_addr_init(void)
{
    const PCI_DEV_INFO_S* i210_dev;
    struct pci_dev *pdev;
    dma_addr_t dma;

    i210_dev = get_mac_dev_info(0);
    pdev = (struct pci_dev *)(i210_dev->pdev);

    dma = dma_map_resource(&(pdev->dev), g_rxRingBase, TOTAL_SIZE,
        DMA_BIDIRECTIONAL, 0);
    if (dma_mapping_error(&(pdev->dev), dma)) {
        return -EAGAIN;
    }

    g_rxRingBusBase = dma;
    g_rxDmaBusBase = g_rxRingBusBase + RX_RING_SIZE;
    g_txRingBusBase = g_rxDmaBusBase + RX_DMA_SIZE;
    g_txDmaBusBase = g_txRingBusBase + TX_RING_SIZE;
    g_macDevBusBase = g_txDmaBusBase + TX_DMA_SIZE;

    return 0;
}

void bus_addr_destory(u32 dev_id)
{
    const PCI_DEV_INFO_S* i210_dev;
    struct pci_dev *pdev;

    i210_dev = get_mac_dev_info(dev_id);
    pdev = (struct pci_dev *)(i210_dev->pdev);
    if (g_rxRingBusBase != 0) {
        dma_unmap_resource(&(pdev->dev), g_rxRingBusBase, TOTAL_SIZE,
            DMA_BIDIRECTIONAL, 0);
        g_rxRingBusBase = 0;
    }
}

#define MEM_ATTR_UNCACHE_RWX    \
    (_PAGE_PRESENT | _PAGE_RW | _PAGE_PCD | _PAGE_ACCESSED | _PAGE_DIRTY)
#define MEM_ATTR_CACHE_RWX      \
    (_PAGE_PRESENT | _PAGE_RW | _PAGE_ACCESSED | _PAGE_DIRTY)
#define MEM_ATTR_CACHE_RX       \
    (_PAGE_PRESENT | _PAGE_ACCESSED | _PAGE_DIRTY)
#define MEM_ATTR_WC_RWX         \
    (_PAGE_PRESENT | _PAGE_RW | _PAGE_ACCESSED | _PAGE_DIRTY | _PAGE_PWT)

#define PAGE_SIZE_2M    0x200000
#define PAGE_SIZE_4K    0x1000
enum {
    BOOT_TABLE = 0,
    PAGE_TABLE,
    BAR_TABLE,
    DMA_TABLE,
    SHAREMEM_TABLE,
    LOG_TABLE,
    TEXT_TABLE,
    DATA_TABLE,
    TABLE_MAX
};

typedef struct {
    unsigned long va;
    unsigned long pa;
    unsigned long size;
    unsigned long attr;
    unsigned long page_size;
} mmu_map_info;
/* 注意: 这里要和 mmu_map.c 中的页表保持一致 */
static mmu_map_info clientos_map_info[TABLE_MAX] = {
    {
        // boottable
        .va = 0x0,
        .pa = 0x0,
        .size = 0x1000,
        .attr = MEM_ATTR_CACHE_RWX,
        .page_size = PAGE_SIZE_4K,
    }, {
        // pagetable
        .va = 0xa0000,
        .pa = 0xa0000,
        .size = 0x6000,
        .attr = MEM_ATTR_CACHE_RWX,
        .page_size = PAGE_SIZE_4K,
    }, {
        // bar
        .va = 0xf00008000,
        .pa = 0x0,
        .size = 0x100000,
        .attr = MEM_ATTR_UNCACHE_RWX,
        .page_size = PAGE_SIZE_4K,
    }, {
        // dma
        .va = 0xf00200000,
        .pa = 0x0,
        .size = 0x200000,
        .attr = MEM_ATTR_UNCACHE_RWX,
        .page_size = PAGE_SIZE_2M,
    }, {
        // sharemem
        .va = 0xf00400000,
        .pa = 0x0,
        .size = 0x2000000,
        .attr = MEM_ATTR_UNCACHE_RWX,
        .page_size = PAGE_SIZE_2M,
    }, {
        // log
        .va = 0xf02400000,
        .pa = 0x0,
        .size = 0x200000,
        .attr = MEM_ATTR_UNCACHE_RWX,
        .page_size = PAGE_SIZE_2M,
    }, {
        // text
        .va = 0xf02600000,
        .pa = 0x0,
        .size = 0x400000,
        .attr = MEM_ATTR_CACHE_RX,
        .page_size = PAGE_SIZE_2M,
    }, {
        // data
        .va = 0xf02a00000,
        .pa = 0x0,
        .size = 0x1000000,
        .attr = MEM_ATTR_CACHE_RWX,
        .page_size = PAGE_SIZE_2M,
    }
};

unsigned long calc_dma_phy_addr(unsigned long loadaddr)
{
    int i;
    unsigned long phy_addr = loadaddr;
    for(i = TEXT_TABLE - 1; i >= DMA_TABLE; i--) {
        phy_addr -= (clientos_map_info[i].size >= PAGE_SIZE_2M) ?
            (clientos_map_info[i].size) : (PAGE_SIZE_2M);
        clientos_map_info[i].pa = phy_addr;
    }
    printk(KERN_INFO "dma_phy_addr:%lx\n", clientos_map_info[DMA_TABLE].pa);
    return clientos_map_info[DMA_TABLE].pa;
}

int net_addr_init(void)
{
    unsigned long load_addr_start = get_load_addr_start();
    printk(KERN_INFO "load_addr_start:%lx\n", load_addr_start);

    g_rxRingBase = calc_dma_phy_addr(load_addr_start);
    g_rxDmaBase = g_rxRingBase + RX_RING_SIZE;
    g_txRingBase = g_rxDmaBase + RX_DMA_SIZE;
    g_txDmaBase = g_txRingBase + TX_RING_SIZE;
    g_macDevBase = g_txDmaBase + TX_DMA_SIZE;

    g_rxRingVirtBase = clientos_map_info[DMA_TABLE].va;
    g_rxDmaVirtBase = g_rxRingVirtBase + RX_RING_SIZE;
    g_txRingVirtBase = g_rxDmaVirtBase + RX_DMA_SIZE;
    g_txDmaVirtBase = g_txRingVirtBase + TX_RING_SIZE;
    g_macDevVirtBase = g_txDmaVirtBase + TX_DMA_SIZE;

    g_rxRingHostVirtBase = (u64)memremap(g_rxRingBase, TOTAL_SIZE, MEMREMAP_WT);
    g_rxDmaHostVirtBase = g_rxRingHostVirtBase + RX_RING_SIZE;
    g_txRingHostVirtBase = g_rxDmaHostVirtBase + RX_DMA_SIZE;
    g_txDmaHostVirtBase = g_txRingHostVirtBase + TX_RING_SIZE;
    g_macDevHostVirtBase = g_txDmaHostVirtBase + TX_DMA_SIZE;
    if (g_rxRingHostVirtBase == 0) {
        return -ENOMEM;
    }

    CALLFUNC(bus_addr_init());
    mac_dev_func_hook(0, bus_addr_destory);

    printk(KERN_INFO "base_addr(phy virt bus host):0x(%llx %llx %llx %llx)\n"
        "size(rx_ring rx_dma tx_ring tx_dma):0x(%lx %x %lx %x)",
        g_rxRingBase, g_rxRingVirtBase, g_rxRingBusBase, g_rxRingHostVirtBase,
        RX_RING_SIZE, RX_DMA_SIZE, TX_RING_SIZE, TX_DMA_SIZE);
    return 0;
}

void mac_addr_print(struct mac_dev *macdev)
{
    // 尝试打印网卡的MAC地址
    unsigned char *pmac_addr = (unsigned char*)(macdev->mac_base + 0x5400);
    printk(KERN_INFO "i210: MAC address = %02x:%02x:%02x:%02x:%02x:%02x",
        pmac_addr[0], pmac_addr[1], pmac_addr[2], pmac_addr[3],
        pmac_addr[4], pmac_addr[5]);
}

int net_dma_init(struct mac_dev *dev)
{
    struct mac_queue *q = &dev->queue;
    int i;

    q->rx_desc = (struct mac_rx_desc *)g_rxRingHostVirtBase;
    ASSERT_INT(q->rx_desc != NULL);
    for (i = 0; i < NR_DESC; i++) {
        q->rx_desc[i].buffer_addr = g_rxDmaBusBase + i * DMA_SIZE;
        q->rx_desc[i].writeback = 0;
    }
    q->rx_desc = (struct mac_rx_desc *)g_rxRingVirtBase;
    q->rx_ring_dma = g_rxRingBusBase;
    q->rx_size = RX_RING_SIZE;

    q->tx_desc = (struct mac_tx_desc *)g_txRingHostVirtBase;
    ASSERT_INT(q->tx_desc != NULL);
    for (i = 0; i < NR_DESC; i++) {
        q->tx_desc[i].buffer_addr = g_txDmaBusBase + i * DMA_SIZE;
        q->tx_desc[i].lower.data = 0;
        q->tx_desc[i].upper.data = 0;
    }
    q->tx_desc = (struct mac_tx_desc *)g_txRingVirtBase;
    q->tx_ring_dma = g_txRingBusBase;
    q->tx_size = TX_RING_SIZE;

    q->virt_addr_offset = g_rxRingVirtBase - g_rxRingBase;
    q->bus_addr_offset = g_rxRingBusBase - g_rxRingBase;
    return 0;
}

int mac_irq_disable(struct mac_dev *dev)
{
    u32 val;
    mac_clear(dev, INTEL_E1000_EIAM, 0);
    mac_write(dev, INTEL_E1000_EIMC, 0);
    mac_clear(dev, INTEL_E1000_EIAC, 0);
    mac_write(dev, INTEL_E1000_IAM, 0);
    mac_write(dev, INTEL_E1000_IMC, ~0);
    mac_read(dev, INTEL_E1000_STATUS, &val);

    return 0;
}

#define MASTER_DISABLE_TIMEOUT  800
int mac_disable_pcie_master(struct mac_dev *dev)
{
    int ret;
    mac_set(dev, INTEL_E1000_CTRL, INTEL_E1000_CTRL_GIO_MASTER_DISABLE);
    ret = mac_wait(dev, INTEL_E1000_STATUS,
        INTEL_E1000_STATUS_GIO_MASTER_ENABLE, 0, MASTER_DISABLE_TIMEOUT);
    if (ret) { /* 仅记录即可 */
        printk(KERN_INFO "PCI-E Master disable polling has failed. \n");
    }

    return 0;
}

inline void mac_flush(struct mac_dev *dev)
{
    unsigned int status;
    mac_read(dev, INTEL_E1000_STATUS, &status);
}

int mac_wait_cfg_done(struct mac_dev *dev)
{
    unsigned int mask =
        INTEL_E1000_STATUS_LAN_INIT_DONE | INTEL_E1000_STATUS_PHYRA;

    CALLFUNC(mac_wait(dev, INTEL_E1000_STATUS, mask, mask,
        INTEL_E1000_ICH8_LAN_INIT_TIMEOUT));
    mac_clear(dev, INTEL_E1000_STATUS, mask);

    return 0;
}

int mac_reset(struct mac_dev *dev)
{
    CALLFUNC(mac_disable_pcie_master(dev));

    mac_write(dev, INTEL_E1000_IMC, 0xffffffff);
    mac_write(dev, INTEL_E1000_RCTL, 0);
    mac_write(dev, INTEL_E1000_TCTL, INTEL_E1000_TCTL_PSP);
    mac_flush(dev);
    mdelay(20);

    mac_set(dev, INTEL_E1000_CTRL,
        INTEL_E1000_CTRL_RST | INTEL_E1000_CTRL_PHY_RST);
    mdelay(20);

    CALLFUNC(mac_wait_cfg_done(dev));

    mac_write(dev, INTEL_E1000_IMC, 0xffffffff);
    mac_set(dev, INTEL_E1000_CTRL_EXT, INTEL_E1000_CTRL_EXT_DRV_LOAD);

    return 0;
}

int rxqueue_i210_init(struct mac_dev *dev)
{
    struct mac_queue *q = &dev->queue;
    int reg;

    ASSERT_INT(q->rx_ring_dma != 0);
    ASSERT_INT(q->rx_desc != NULL);
    q->rx_desc_nr = q->rx_size / sizeof(struct mac_rx_desc);
    q->rx_head = 0;
    q->rx_tail = q->rx_desc_nr - 1;

    mac_read(dev, E1000_I210_RXDCTL(0), &reg);
    reg &= ~(1 << 25);
    mac_write(dev, E1000_I210_RXDCTL(0), reg);
    mdelay(10);

    mac_read(dev, E1000_I210_RXDCTL(0), &reg);
    printk(KERN_INFO "E1000_RXDCTL after 0x%x\n", reg);

    reg = 0;
    mac_read(dev, INTEL_E1000_RCTL, &reg);
    reg &= ~(3 << 12);
    reg &= ~(3 << 16);
    reg &= ~(3 << 6);
    reg |= ((1 << 1) | (1 << 15) | (1 << 26));
    mac_set(dev, INTEL_E1000_RCTL, reg);

    mac_write(dev, E1000_I210_RDBAL(0), (unsigned int)q->rx_ring_dma);
    mac_write(dev, E1000_I210_RDBAH(0), (unsigned int)(q->rx_ring_dma >> 32));
    mac_write(dev, E1000_I210_RDLEN(0), q->rx_size);
    mac_write(dev, E1000_I210_RDH(0), q->rx_head);
    mac_write(dev, E1000_I210_RDT(0), q->rx_tail);

    mac_read(dev, E1000_I210_RXDCTL(0), &reg);
    reg |= (1 << 25);
    mac_write(dev, E1000_I210_RXDCTL(0), reg);
    mac_read(dev, E1000_I210_RXDCTL(0), &reg);
    printk(KERN_INFO "E1000_RXDCTL after 0x%x\n", reg);
    mac_write(dev, E1000_I210_RDT(0), q->rx_tail);

    return 0;
}

int txqueue_i210_init(struct mac_dev *dev)
{
    struct mac_queue *q = &dev->queue;
    int reg;

    ASSERT_INT(q->tx_ring_dma != 0);
    ASSERT_INT(q->tx_desc != NULL);
    q->tx_desc_nr = q->tx_size / sizeof(struct mac_tx_desc);
    q->tx_head = q->tx_tail = 0;

    mac_read(dev, E1000_I210_TXDCTL(0), &reg);
    printk(KERN_INFO "E1000_TXDCTL before 0x%x\n", reg);
    reg &= ~(1 << 25);
    mac_write(dev, E1000_I210_TXDCTL(0), reg);
    mac_read(dev, E1000_I210_TXDCTL(0), &reg);
    printk(KERN_INFO "E1000_TXDCTL after 0x%x\n", reg);

    mac_write(dev, E1000_I210_TDBAL(0),
        (unsigned int)(q->tx_ring_dma & 0xffffffff));
    mac_write(dev, E1000_I210_TDBAH(0),
        (unsigned int)((q->tx_ring_dma >> 32) & 0xffffffff));
    mac_write(dev, E1000_I210_TDLEN(0), q->tx_size);

    mac_read(dev, E1000_I210_TXDCTL(0), &reg);
    printk(KERN_INFO "E1000_TXDCTL before 0x%x\n", reg);
    reg &= ~(0x1f << 16);
    reg |= (1 << 16);
    mac_write(dev, E1000_I210_TXDCTL(0), reg);
    mac_read(dev, E1000_I210_TXDCTL(0), &reg);
    printk(KERN_INFO "E1000_TXDCTL after 0x%x\n", reg);

    mac_write(dev, E1000_I210_TDH(0), 0);
    mac_write(dev, E1000_I210_TDT(0), 0);

    mac_read(dev, E1000_I210_TXDCTL(0), &reg);
    printk(KERN_INFO "E1000_TXDCTL before 0x%x\n", reg);
    reg |= (1 << 25);
    mac_write(dev, E1000_I210_TXDCTL(0), reg);
    mac_read(dev, E1000_I210_TXDCTL(0), &reg);
    printk(KERN_INFO "E1000_TXDCTL after 0x%x\n", reg);

    mac_set(dev, INTEL_E1000_TCTL, INTEL_E1000_TCTL_EN | INTEL_E1000_TCTL_RTLC);

    return 0;
}

int queue_i210_init(struct mac_dev *dev)
{
    CALLFUNC(rxqueue_i210_init(dev));
    CALLFUNC(txqueue_i210_init(dev));
    return 0;
}

void put_hw_semaphore(struct mac_dev *dev)
{
    u32 swsm;
    mac_read(dev, INTEL_E1000_SWSM, &swsm);
    swsm &= ~(INTEL_E1000_SWSM_SMBI | INTEL_E1000_SWSM_SWESMBI);
    mac_write(dev, INTEL_E1000_SWSM, swsm);
}

int get_hw_semaphore(struct mac_dev *dev)
{
    static u32 clear_semaphore_once = 0;
    u32 i = 0;
    u32 swsm;
    s32 timeout = 0x8000;

    /* Get the SW semaphore */
    while (i < timeout) {
        mac_read(dev, INTEL_E1000_SWSM, &swsm);
        if (!(swsm & INTEL_E1000_SWSM_SMBI))
            break;

        udelay(50);
        i++;
    }

    if (i == timeout && clear_semaphore_once == 0) {
        printk(KERN_INFO "get_hw_semaphore timeout once.\n");
        clear_semaphore_once++;
        put_hw_semaphore(dev);

        for (i = 0; i < timeout; i++) {
            mac_read(dev, INTEL_E1000_SWSM, &swsm);
            if (!(swsm & INTEL_E1000_SWSM_SMBI))
                break;

            udelay(50);
        }

        if (i == timeout) {
            printk(KERN_INFO "get_hw_semaphore Driver can't access device"
                " - SMBI bit is set.\n");
            return -1;
        }
    }

    /* Get the FW semaphore. */
    for (i = 0; i < timeout; i++) {
        mac_read(dev, INTEL_E1000_SWSM, &swsm);
        mac_write(dev, INTEL_E1000_SWSM, swsm | INTEL_E1000_SWSM_SWESMBI);

        /* Semaphore acquired if bit latched */
        mac_read(dev, INTEL_E1000_SWSM, &swsm);
        if (swsm & INTEL_E1000_SWSM_SWESMBI)
            break;

        udelay(50);
    }

    if (i == timeout) {
        /* Release semaphores */
        put_hw_semaphore(dev);
        printk(KERN_INFO "get_hw_semaphore Driver can't access the NVM\n");
        return -1;
    }

    return 0;
}

int release_swfw_sync(struct mac_dev *dev, u32 mask)
{
    u32 swfw_sync;
    while (get_hw_semaphore(dev))
        ; /* Empty */

    mac_read(dev, INTEL_E1000_SW_FW_SYNC, &swfw_sync);
    swfw_sync &= ~mask;
    mac_write(dev, INTEL_E1000_SW_FW_SYNC, swfw_sync);

    put_hw_semaphore(dev);
    return 0;
}

int mac_init(struct mac_dev *dev)
{
    u32 val;
    mac_read(dev, INTEL_E1000_CTRL_EXT, &val);
    mac_read(dev, INTEL_E1000_FWSM, &val);
    mac_read(dev, INTEL_E1000_EECD, &val);
    mac_read(dev, INTEL_E1000_CTRL_EXT, &val);
    mac_write(dev, INTEL_E1000_CTRL_EXT, val);
    mac_read(dev, INTEL_E1000_STATUS, &val);

    release_swfw_sync(dev, 0xffffffff);

    mac_irq_disable(dev);
    mac_reset(dev);
    queue_i210_init(dev);

    return 0;
}

int mac_dev_info_init(void)
{
    struct mac_dev *macdev;
    const PCI_DEV_INFO_S* mac_dev_info = get_mac_dev_info(0);
    if (mac_dev_info == NULL) {
        printk(KERN_INFO "mac_dev_info_init mac_dev_info is null\n");
        return -1;
    }

    /* 1. 全局变量初始化：几种基地址的初始化 */
    CALLFUNC(net_addr_init());

    /* 2. macdev指向存储空间结构体，并初始化pci设备的基地址 */
    macdev = (struct mac_dev *)g_macDevHostVirtBase;
    macdev->mac_base = (u64)(mac_dev_info->io_virt);

    /* 3. pci设备软、硬件的初始化：mac地址打印, dma相关空间配置, 硬件初始化 */
    mac_addr_print(macdev);
    CALLFUNC(net_dma_init(macdev));
    CALLFUNC(mac_init(macdev));
    mdelay(1000);

    /* 4. 将pci设备的物理地址传递给mcs_km.ko, 配置rtos.bin运行页表时使用 */
    set_bar_addr((unsigned long)mac_dev_info->io_phy);
    /* rtos侧的访问pci设备使用的虚拟地址 */
    macdev->mac_base = clientos_map_info[BAR_TABLE].va;

    /* 5. dump mac_dev, 用于比较rtos侧是否正确获取信息 */
    mac_dev_dump(macdev);
    return 0;
}

#ifdef MACDEV_OPERATE
static inline void plat_writel(u32 val, u32 *addr)
{
    *addr = val;
}

static inline u32 plat_readl(u32 *addr)
{
    return *addr;
}

int mac_read(struct mac_dev *dev, int offset, u32 *val)
{
    *val = plat_readl((u32*)(size_t)(dev->mac_base + offset));
    return 0;
}

int mac_write(struct mac_dev *dev, int offset, u32 val)
{
    plat_writel(val, (u32*)(size_t)(dev->mac_base + offset));
    return 0;
}

int mac_wait(struct mac_dev *dev, int offset, unsigned int bitmask,
    unsigned int expect, int timeout)
{
    unsigned int val;
    while (timeout--) {
        mdelay(10);
        CALLFUNC(mac_read(dev, offset, &val));
        if ((val & bitmask) == expect)
            return 0;
    }
    printk(KERN_INFO "offset=%x bitmask=%x expect=%x timeout=%d val=%x\n",
        offset, bitmask, expect, timeout, val);
    return -1;
}

int mac_op(struct mac_dev *dev, int offset, unsigned int clear,
    unsigned int set)
{
    unsigned int val;
    CALLFUNC(mac_read(dev, offset, &val));
    val &= ~clear;
    val |= set;
    CALLFUNC(mac_write(dev, offset, val));
    return 0;
}

int mac_set(struct mac_dev *dev, int offset, unsigned int bits)
{
    return mac_op(dev, offset, 0, bits);
}

int mac_clear(struct mac_dev *dev, int offset, unsigned int bits)
{
    return mac_op(dev, offset, bits, 0);
}
#endif

void mac_dev_dump(struct mac_dev *macdev)
{
    if (macdev == NULL)
        return;

    printk(KERN_INFO "macdev: queue{%llx %llx %llx %x %llx %x,\n"
    " 0x%llx, %x %x %x,\n 0x%llx, %x %x %x}\n 0x%llx 0x%llx %x %x %x",
        macdev->queue.virt_addr_offset, macdev->queue.bus_addr_offset,
        macdev->queue.rx_ring_dma, macdev->queue.rx_size,
        macdev->queue.tx_ring_dma, macdev->queue.tx_size,
        (long long unsigned int)macdev->queue.rx_desc,
        macdev->queue.rx_desc_nr, macdev->queue.rx_head, macdev->queue.rx_tail,
        (long long unsigned int)macdev->queue.tx_desc,
        macdev->queue.tx_desc_nr, macdev->queue.tx_head, macdev->queue.tx_tail,
        macdev->mac_base, macdev->pci_cfg_base, macdev->pm_cap, macdev->msi_cap,
        macdev->locked);
}
