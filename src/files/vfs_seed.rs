//! Seed Markdown note templates for the in-memory VFS.

pub(super) const NOTE_WELCOME: &str = r#"# 潮州周末漫游计划

周五傍晚出发的两天两夜老城慢游，住在牌坊街附近的小院民宿，把节奏放慢，专心喝茶、散步与寻味街头小吃。

> [!TIP]
> 提前在周三锁好周五傍晚往返潮汕站的高铁票；韩江边入夜微凉，记得在随身包里塞一件薄外套。

## 周五至周日行程

- [x] **周五傍晚抵达潮汕站** — 搭乘接驳专线进古城，办理入住后夜游牌坊街与广济楼夜景
- [x] **周六上午广济桥与工夫茶** — 早起看湘子桥启闭浮桥，转进载阳茶馆听弦诗乐、喝一泡鸭屎香单丛
- [ ] **周日上午西马路早市与返程** — 逛老街早市买腐乳饼与手锤牛肉丸，午后慢悠悠喝杯甘草水果再返程

## 行程与预算表

| 时间 | 安排 | 地点 / 备注 | 预算 |
| :--- | :--- | :--- | :--- |
| 周五 20:30 | 官塘鲜牛肉火锅晚餐 | 牌坊街南门老店，必点吊龙与胸口朥 | ¥180 |
| 周六 09:30 | 广济桥登桥 + 老茶馆早茶 | 备好身份证检票，上午光线最适合拍照 | ¥120 |
| 周六 15:00 | 甲第巷古民居与手作陶艺 | 巷口老铺尝一碗热腾腾的鸭母捻 | ¥90 |
| 周日 10:00 | 西马路早市采购手信 | 腐乳饼、凤凰单丛茶叶、老药桔 | ¥260 |

## 街头风味记录

- **官塘牛肉火锅** — 清汤锅底只放萝卜与玉米，现切匙柄涮八秒蘸沙茶酱最鲜甜
- **胡荣泉腐乳饼与鸭母捻** — 百年老字号甜汤配刚出炉的腐乳饼，蒜香与南乳脂香刚刚好
- **凤凰单丛茶** — 在茶铺试了蜜兰香与桂花香，回甘绵长，适合带两罐回家周末慢慢冲泡

> 老城的迷人之处不在赶景点，而在巷弄深处随时飘出的一缕炭火茶香。
"#;

pub(super) const NOTE_ARCH: &str = r#"# 出行行李与待办

两天两夜的短途周末游尽量轻装简行，一只登机双肩包加一只帆布托特袋就能装下全部随身物品。

## 随身包清单

- [x] 身份证、高铁车票信息确认、小额零钱
- [x] 手机充电器、磁吸充电宝与耳机
- [ ] 晴雨两用折叠伞与薄针织开衫
- [ ] 胶片相机（装好两卷 400 度彩色负片）与便携旅行主人杯

## 出发前家里待办

1. 给阳台迷迭香、龟背竹和客厅琴叶榕浇透水，移到散射光处
2. 拔掉烧水壶、咖啡机与书桌排插电源，关好纱窗
3. 加满自动喂猫器干粮与流动饮水机，清理猫砂盆
4. 出门前顺手带走厨房与玄关垃圾袋

## 伴手礼备忘

| 亲友 | 想带的特产 | 店铺位置 |
| :--- | :--- | :--- |
| 爸妈 | 蜜兰香凤凰单丛茶（春茶两罐） | 牌坊街中段老茶行 |
| 工作室同事 | 现烤腐乳饼与手打牛肉丸 | 西马路老字号饼铺 |
| 邻居晓林 | 老香黄佛手果与甘草话梅 | 开元寺街口凉果店 |
"#;

pub(super) const NOTE_GUIDE: &str = r#"# 周末家庭咖啡馆

不赶时间的周日早晨，拉开亚麻窗帘让阳光洒进餐桌，磨一杯果香明亮的手冲咖啡，再烤两片外脆内软的酸种欧包。

> [!NOTE]
> 这周开封的浅中烘埃塞俄比亚豆子已经养豆满 10 天，风味正值巅峰；手冲水温控制在 **92°C**，分三段注水最能突出柑橘与白花香气。

## 手冲配方记录

| 豆子 | 粉水比 | 水温 | 风味记录 |
| :--- | :--- | :--- | :--- |
| 埃塞俄比亚 耶加雪菲 水洗 | 1 : 15（16g 粉 / 240ml 水） | 92°C | 佛手柑、茉莉花香，尾韵像冰糖柠檬茶 |
| 哥伦比亚 粉红波旁 日晒 | 1 : 16（15g 粉 / 240ml 水） | 91°C | 熟桃子与草莓果酱甜感，油脂感圆润 |
| 云南保山 小粒铁皮卡 | 1 : 15（18g 粉 / 270ml 水） | 89°C | 烤坚果、红糖甜香，适合加冰牛奶做拿铁 |

## 周日早午餐菜单

- **牛油果水波蛋酸种吐司** — 酸种切片平底锅黄油煎脆，铺满黑胡椒牛油果泥与流心水波蛋
- **迷迭香烤小番茄与口蘑** — 橄榄油、海盐与新鲜百里香拌匀，烤箱 200°C 烘烤十二分钟
- **冰博克 Dirty 咖啡** — 冰镇厚牛乳垫底，直接萃取双份浓缩缓慢淋在表面，大口喝下冷热交融的层次
"#;

pub(super) const NOTE_WELCOME_EN: &str = r#"# Weekend Getaway Plan

A slow two-night trip to Kyoto from Friday evening to Sunday night—staying in a quiet wooden machiya near Higashiyama, waking up early for riverside walks, morning coffee, and neighborhood pottery shops.

> [!TIP]
> Reserve window seats on the Friday evening train by Wednesday, and pack a light cardigan for the cool evening breeze along the Kamo River.

## Weekend Itinerary

- [x] **Friday evening arrival** — Check into the guesthouse, stroll down Pontocho alley, and grab late-night yuzu ramen
- [x] **Saturday morning riverside & tea house** — Early walk along the Kamo River stepping stones, followed by roasted green tea sweets in Gion
- [ ] **Sunday morning market & return trip** — Browse Nishiki Market for pickled vegetables and ceramic mugs before the afternoon train home

## Schedule & Budget

| Time | Activity | Spot / Notes | Budget |
| :--- | :--- | :--- | :--- |
| Fri 20:00 | Late Dinner & Alley Walk | Yuzu shio ramen near Sanjo station | $28 |
| Sat 08:30 | Morning Coffee & Pastry | Independent roaster by the canal, outdoor bench | $18 |
| Sat 14:30 | Kiyomizu Pottery Lane | Hand-thrown stoneware cups and Uji matcha | $65 |
| Sun 09:30 | Morning Market & Souvenirs | Sesame snacks, roasted tea leaves, linen coasters | $45 |

## Local Favorites

- **Morning Market Coffee** — Freshly roasted pour-over enjoyed by the open window while the street stalls set up
- **Kamo River Walk** — Watching herons by the stepping stones with a warm red-bean pastry in hand
- **Pottery Street Finds** — Small family kilns tucked into quiet side lanes with speckled glaze teacups

> The best part of a weekend trip is leaving half the afternoon unplanned so you can follow whatever quiet lane catches your eye.
"#;

pub(super) const NOTE_ARCH_EN: &str = r#"# Luggage & Errands

Packing light for a two-night weekend trip: everything fits neatly into one carry-on backpack and a canvas tote bag so walking from the station to the inn feels effortless.

## Carry-On Checklist

- [x] Train tickets, transit card, and a small coin pouch
- [x] Phone charger, portable power bank, and sunglasses
- [ ] Compact folding umbrella and a breathable linen shirt
- [ ] Film camera with two rolls of 400-speed color film

## Before Leaving Home

1. Water the Boston fern and monstera on the balcony and move them out of direct midday sun
2. Refill the automatic cat feeder, refresh the water fountain, and tidy the litter box
3. Unplug the espresso machine, toaster, and desk lamp
4. Take out the kitchen compost and recycling bins on the way to the station

## Souvenir Ideas

| For | Gift Idea | Where to Find |
| :--- | :--- | :--- |
| Mom & Dad | First-harvest roasted green tea tin | Historic tea shop on Teramachi Street |
| Studio Friends | Matcha butter cookies & sesame brittle | Nishiki Market confectionery stall |
| Maya | Speckled ceramic espresso cup | Small pottery studio along Gojozaka |
"#;

pub(super) const NOTE_GUIDE_EN: &str = r#"# Home Cafe & Recipes

Slow Sunday mornings at home: open the kitchen window, grind fresh beans for a bright pour-over, and toast thick slices of country sourdough while the kettle comes up to temperature.

> [!NOTE]
> Use filtered water at **92°C** and a **1:15 brew ratio** for medium-light roasts; letting the beans rest 10 days after roast date brings out the clearest floral and stone-fruit sweetness.

## Pour-Over Log

| Beans | Ratio | Water Temp | Tasting Notes |
| :--- | :--- | :--- | :--- |
| Ethiopia Yirgacheffe Washed | 1 : 15 (16g / 240ml) | 92°C | Bergamot, jasmine blossom, crisp Meyer lemon finish |
| Colombia Pink Bourbon Natural | 1 : 16 (15g / 240ml) | 91°C | Ripe white peach, strawberry jam, silky honey body |
| Guatemala Huehuetenango | 1 : 15 (18g / 270ml) | 90°C | Toasted hazelnut, milk chocolate, sweet caramel aftertaste |

## Sunday Brunch Menu

- **Toasted Sourdough with Whipped Ricotta & Figs** — Thick-cut sourdough brushed with olive oil, topped with lemon-zest ricotta, fresh figs, and wildflower honey
- **Soft-Scrambled Eggs with Chives** — Cooked low and slow with salted butter until glossy and spooned over warm toast
- **Iced Oat Latte** — Double shot of espresso poured over ice and creamy barista oat milk with a pinch of cinnamon
"#;
