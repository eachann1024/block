import math
BG="#efeeeb"; INK="#2a2b2d"; W="#f7f6f3"
def tile(name, body):
    svg=f'''<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 1024 1024" width="1024" height="1024">
<defs><clipPath id="t"><rect x="100" y="100" width="824" height="824" rx="185"/></clipPath></defs>
<g clip-path="url(#t)"><rect x="0" y="0" width="1024" height="1024" fill="{BG}"/>
{body}
</g></svg>'''
    open(name,"w").write(svg)

# A1 叼笔的猫
tile("a1-cat-pencil.svg", f'''
<g fill="{INK}" stroke="{INK}" stroke-width="40" stroke-linejoin="round">
 <path d="M228 560 L262 262 L452 420 Z"/><path d="M796 560 L762 262 L572 420 Z"/>
</g>
<ellipse cx="512" cy="700" rx="340" ry="290" fill="{INK}"/>
<path d="M290 360 L300 300 L360 350" fill="none" stroke="{W}" stroke-width="16" stroke-linecap="round" stroke-linejoin="round"/>
<g fill="none" stroke="{W}" stroke-width="28" stroke-linecap="round">
 <path d="M350 615 q55 -60 110 0"/><path d="M564 615 q55 -60 110 0"/>
</g>
<path d="M486 676 h52 l-26 30 z" fill="{W}" stroke="{W}" stroke-width="10" stroke-linejoin="round"/>
<g transform="rotate(-7 512 790)">
 <g stroke="{INK}" stroke-width="18" stroke-linejoin="round">
  <path d="M120 790 L230 752 H800 V828 H230 Z" fill="{W}"/>
  <rect x="800" y="752" width="110" height="76" rx="14" fill="{INK}"/>
 </g>
 <path d="M120 790 L160 776 V804 Z" fill="{INK}"/>
 <path d="M230 752 V828" stroke="{INK}" stroke-width="14"/>
 <rect x="830" y="752" width="18" height="76" fill="{W}"/>
</g>
<g fill="none" stroke="{W}" stroke-width="20" stroke-linecap="round">
 <path d="M440 735 q22 -14 40 4"/><path d="M584 735 q-22 -14 -40 4"/>
</g>
''')

# A2 喷出文字的鲸
tile("a2-whale-spout.svg", f'''
<path d="M60 640 C170 450 520 430 690 560 C760 612 800 600 840 520 L860 470 C880 440 940 420 980 430 L940 520 C1000 530 1010 580 1000 600 L900 600 C880 660 900 760 960 1000 L60 1000 Z" fill="{INK}" stroke="{INK}" stroke-width="20" stroke-linejoin="round"/>
<circle cx="250" cy="680" r="20" fill="{W}"/>
<path d="M100 760 C200 800 330 790 420 740" fill="none" stroke="{W}" stroke-width="22" stroke-linecap="round"/>
<g fill="none" stroke="{W}" stroke-width="16" stroke-linecap="round" opacity=".9">
 <path d="M160 850 C280 870 420 860 520 820"/><path d="M190 905 C320 925 470 910 590 870"/>
</g>
<g fill="none" stroke="{INK}" stroke-linecap="round">
 <path d="M420 470 V360" stroke-width="30"/>
 <path d="M330 300 H520" stroke-width="34"/>
 <path d="M290 230 H560" stroke-width="34"/>
 <path d="M350 160 H480" stroke-width="34"/>
 <path d="M520 160 H540" stroke-width="34"/>
</g>
''')

# A3 背便签的刺猬
cx,cy,rx,ry=720,980,520,600
pts=[]
n=22
a0,a1=math.radians(196),math.radians(355)
for i in range(n*2+1):
    a=a0+(a1-a0)*i/(n*2)
    r=1.0 if i%2==0 else 1.17
    pts.append((cx+rx*r*math.cos(a), cy+ry*r*math.sin(a)))
spk=" ".join(f"L{x:.0f} {y:.0f}" for x,y in pts)
tile("a3-hedgehog-note.svg", f'''
<path d="M1000 1000 L230 1000 C230 900 200 830 150 770 C210 700 260 690 {pts[0][0]:.0f} {pts[0][1]:.0f} {spk} L1000 700 Z" fill="{INK}" stroke="{INK}" stroke-width="14" stroke-linejoin="round"/>
<g transform="rotate(12 600 330)">
 <rect x="510" y="230" width="190" height="200" rx="18" fill="{W}" stroke="{INK}" stroke-width="18"/>
 <g stroke="{INK}" stroke-width="18" stroke-linecap="round"><path d="M550 330 H660"/><path d="M550 380 H630"/></g>
 <path d="M585 250 L605 160 L625 250 Z" fill="{INK}" stroke="{INK}" stroke-width="10" stroke-linejoin="round"/>
 <path d="M575 262 H635" stroke="{INK}" stroke-width="22" stroke-linecap="round"/>
</g>
<circle cx="150" cy="770" r="26" fill="{INK}"/>
<path d="M290 745 q30 26 62 0" fill="none" stroke="{W}" stroke-width="22" stroke-linecap="round"/>
<path d="M240 830 q40 26 80 6" fill="none" stroke="{W}" stroke-width="16" stroke-linecap="round"/>
''')

EAGLE=("M-20 520 L210 420 L120 330 L350 300 L300 210 C470 190 640 230 760 330 "
       "C860 410 900 520 880 610 C870 660 850 700 815 745 C812 700 800 672 770 655 "
       "C720 630 640 620 600 650 C570 675 575 720 600 770 L760 1100 L-20 1100 Z")
def eye(dx=0,dy=0):
    return f'''<g transform="translate({dx} {dy})"><path d="M520 430 L780 560" stroke="{W}" stroke-width="22" stroke-linecap="round"/>
<path d="M610 478 C600 530 650 560 720 548" fill="none" stroke="{W}" stroke-width="20" stroke-linecap="round"/>
<circle cx="668" cy="520" r="20" fill="{W}"/></g>'''

# B1 衔笔尖的鹰
tile("b1-eagle-nib.svg", f'''
<path d="M735 880 C790 900 830 880 870 850" fill="none" stroke="{INK}" stroke-width="24" stroke-linecap="round"/>
<path d="{EAGLE.replace('L760 1100','L520 1100')}" fill="{INK}" stroke="{INK}" stroke-width="16" stroke-linejoin="round"/>
{eye()}
<g transform="translate(-10 -30) rotate(-12 700 720)">
 <path d="M620 650 H780 V720 L700 900 L620 720 Z" fill="{W}" stroke="{INK}" stroke-width="20" stroke-linejoin="round"/>
 <path d="M700 760 V880" stroke="{INK}" stroke-width="14" stroke-linecap="round"/>
 <circle cx="700" cy="745" r="16" fill="{INK}"/>
</g>
''')

# B2 盯住光标的鹰
tile("b2-eagle-cursor.svg", f'''
<g transform="translate(-170 40)"><path d="{EAGLE}" fill="{INK}" stroke="{INK}" stroke-width="16" stroke-linejoin="round"/>{eye()}</g>
<g stroke="{INK}" stroke-linecap="round" fill="none">
 <path d="M800 560 V860" stroke-width="30"/>
 <path d="M760 560 H840 M760 860 H840" stroke-width="26"/>
</g>
<g stroke="{INK}" stroke-width="16" stroke-linecap="round" opacity=".55">
 <path d="M870 640 l40 -24"/><path d="M880 710 h46"/><path d="M870 780 l40 24"/>
</g>
''')

# B3 从方块里钻出的鹰
tile("b3-eagle-hatch.svg", f'''
<path d="M120 700 L512 560 L904 700 L512 840 Z" fill="{INK}" stroke="{W}" stroke-width="22" stroke-linejoin="round"/>
<g transform="translate(60 40) scale(.68) translate(120 0)">
<path d="{EAGLE}" fill="{INK}" stroke="{W}" stroke-width="40" stroke-linejoin="round" paint-order="stroke"/>
{eye()}
</g>
<path d="M120 700 L512 840 L904 700 V1100 H120 Z" fill="{INK}"/>
<path d="M120 700 L512 840 L904 700 M512 840 V1100" fill="none" stroke="{W}" stroke-width="22" stroke-linejoin="round"/>
''')
