#ifndef _I210_ETH_H_
#define _I210_ETH_H_

/********这里涉及多个地址概念，如下图示********
 *  本驱动代码需要将rtos侧的地址预配置好        
 *       linux/oe      |      rtos_ck        
 *        ↓↓↓          |       ↓↓↓           
 *    virt_addr_host   |    virt_addr_guest  
 *---------------------|---------------------
 *                  phy_addr                 
 *-------------------------------------------
 *                bus_addr(dma_addr)         
 *                    ↑↑↑                    
 *                  i210_eth                 
 */

#define RESERVE_MEM_BASE_CK 0x400000000ULL

#define RESERVE_MEM_BASE 0x400200000ULL
#define NR_DESC 128
#define DMA_SIZE 2048

#define RX_RING_BASE RESERVE_MEM_BASE
#define RX_RING_SIZE (sizeof(struct mac_rx_desc) * NR_DESC)
#define RX_DMA_BASE (RX_RING_BASE + RX_RING_SIZE)
#define RX_DMA_SIZE (DMA_SIZE * NR_DESC)

#define TX_RING_BASE (RX_DMA_BASE + RX_DMA_SIZE)
#define TX_RING_SIZE (sizeof(struct mac_tx_desc) * NR_DESC)
#define TX_DMA_BASE (TX_RING_BASE + TX_RING_SIZE)
#define TX_DMA_SIZE (DMA_SIZE * NR_DESC)

#define MAC_DEV_BASE (TX_DMA_BASE + TX_DMA_SIZE)
#define MAC_DEV_SIZE (sizeof(struct mac_dev))
#define TOTAL_SIZE (MAC_DEV_BASE + MAC_DEV_SIZE - RESERVE_MEM_BASE)

#define E1000_I210_RDBAL(_n) (0x0c000 + ((_n) * 0x40))
#define E1000_I210_RDBAH(_n) (0x0c004 + ((_n) * 0x40))
#define E1000_I210_RDLEN(_n) (0x0c008 + ((_n) * 0x40))
#define E1000_I210_RDH(_n)   (0x0c010 + ((_n) * 0x40))
#define E1000_I210_RDT(_n)   (0x0c018 + ((_n) * 0x40))
#define E1000_I210_RXDCTL(_n) (0x0c028 + ((_n) * 0x40))

#define E1000_I210_TDBAL(_n) (0x0e000 + ((_n) * 0x40))
#define E1000_I210_TDBAH(_n) (0x0e004 + ((_n) * 0x40))
#define E1000_I210_TDLEN(_n) (0x0e008 + ((_n) * 0x40))
#define E1000_I210_TDH(_n)   (0x0e010 + ((_n) * 0x40))
#define E1000_I210_TDT(_n)   (0x0e018 + ((_n) * 0x40))
#define E1000_I210_TXDCTL(_n) (0x0e028 + ((_n) * 0x40))

#define E1000_RDBAL(_n)     E1000_I210_RDBAL(_n)
#define E1000_RDBAH(_n)     E1000_I210_RDBAH(_n)
#define E1000_RDLEN(_n)     E1000_I210_RDLEN(_n)
#define E1000_RDH(_n)       E1000_I210_RDH(_n)
#define E1000_RDT(_n)       E1000_I210_RDT(_n)
#define E1000_RXDCTL(_n)    E1000_I210_RXDCTL(_n)

#define E1000_TDBAL(_n)     E1000_I210_TDBAL(_n)
#define E1000_TDBAH(_n)     E1000_I210_TDBAH(_n)
#define E1000_TDLEN(_n)     E1000_I210_TDLEN(_n)
#define E1000_TDH(_n)       E1000_I210_TDH(_n)
#define E1000_TDT(_n)       E1000_I210_TDT(_n)

typedef void (*pfn_free_dma_map_t)(unsigned int dev_id);
typedef struct {
    unsigned int dev_id;
    void __iomem* io_phy;
    void __iomem* io_virt;
    void *pdev; /* struct pci_dev */
    pfn_free_dma_map_t pfn_free_dma_map;
} PCI_DEV_INFO_S;

#define I210_DEV_MAX_NUM 4 /* 最大支持i210设备个数 */
const PCI_DEV_INFO_S* get_mac_dev_info(u32 dev_id);
void mac_dev_func_hook(u32 dev_id, pfn_free_dma_map_t fn_free_dma_map);

/* Transmit Descriptor */
struct mac_tx_desc {
    unsigned long long buffer_addr; /* Address of the descriptor's data buf */
    union {
        unsigned int data;
        struct {
            unsigned short length;  /* Data buffer length */
            unsigned char cso;  /* Checksum offset */
            unsigned char cmd;  /* Descriptor control */
#define E1000_TXD_CMD_EOP 0x01   /* End of Packet */
#define E1000_TXD_CMD_IFCS 0x02  /* Insert FCS (Ethernet CRC) */
#define E1000_TXD_CMD_IC 0x04    /* Insert Checksum */
#define E1000_TXD_CMD_RS 0x08    /* Report Status */
#define E1000_TXD_CMD_RPS 0x10   /* Report Packet Sent */
#define E1000_TXD_CMD_DEXT 0x20  /* Desc extension (0 = legacy) */
#define E1000_TXD_CMD_VLE 0x40   /* Add VLAN tag */
#define E1000_TXD_CMD_IDE 0x80   /* Interrupt Delay Enable */
        } flags;
    } lower;
    union {
        unsigned int data;
        struct {
            unsigned char status;   /* Descriptor status */
            unsigned char css; /* Checksum start */
            unsigned short speial;
        } fields;
    } upper;
};

/* Receive Descriptor - Extended */
struct mac_rx_desc {
    unsigned long long buffer_addr;
    unsigned long long writeback; /* writeback */
};

struct mac_queue {
    unsigned long long virt_addr_offset;
    unsigned long long bus_addr_offset;
    unsigned long long rx_ring_dma;
    unsigned int rx_size;
    unsigned long long tx_ring_dma;
    unsigned int tx_size;
    struct mac_rx_desc *rx_desc;
    int rx_desc_nr;
    int rx_head;
    int rx_tail;
    struct mac_tx_desc *tx_desc;
    int tx_desc_nr;
    int tx_head;
    int tx_tail;
};

typedef struct mac_dev {
    struct mac_queue queue;
    unsigned long long mac_base;
    unsigned long long pci_cfg_base;
    unsigned int pm_cap;
    unsigned int msi_cap;
    int locked;
} MAC_DEV_S;

#if 1 /* comm func */
#define ASSERT_INT(exp) do { \
    if (!(exp)) { \
        printk(KERN_INFO "assert int "#exp" false!!"); \
        return -1;\
    } \
} while(0)

#define __CALLFUNC(func, op, val) do { \
    int ret = func;\
    if (ret) { \
        printk(KERN_INFO "call "#func" faild err=%d\n", ret); \
        op val;\
    } \
 } while(0)

#define CALLFUNC(func) __CALLFUNC(func, return, ret)
#endif /* comm func */

#define MACDEV_OPERATE
#ifdef MACDEV_OPERATE
int mac_read(struct mac_dev *dev, int offset, u32 *val);
int mac_write(struct mac_dev *dev, int offset, u32 val);
int mac_wait(struct mac_dev *dev, int offset, unsigned int bitmask,
    unsigned int expect, int timeout);
int mac_op(struct mac_dev *dev, int offset, unsigned int clear,
    unsigned int set);
int mac_set(struct mac_dev *dev, int offset, unsigned int bits);
int mac_clear(struct mac_dev *dev, int offset, unsigned int bits);
#endif

void mac_dev_dump(struct mac_dev *macdev);
int mac_dev_info_init(void);

#endif