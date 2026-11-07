use super::{Block, Kind};
use serde::{Deserialize, Deserializer, Serialize, Serializer};
use std::{ops::Index, sync::Arc};

type Link = Option<Arc<Node>>;
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
struct Run { indent: usize, count: u64, start: u64 }
fn single_run(b: &Block) -> Option<Run> {
    if let Some(code) = &b.meta.list_code {
        return Some(Run { indent: code.parent_indent.saturating_add(1), count: 0, start: 0 });
    }
    matches!(b.kind, Kind::Numbered | Kind::Bullet | Kind::Todo).then_some(Run {
        indent: b.meta.indent, count: u64::from(b.kind == Kind::Numbered), start: b.meta.list_start.unwrap_or(1),
    })
}
fn merge_run(a: Run, b: Run) -> Option<Run> {
    Some(if a.indent < b.indent { a } else if b.indent < a.indent { b }
    else if a.count > 0 && b.count > 0 { Run { indent: a.indent, count: a.count + b.count, start: a.start } }
    else { Run { indent: a.indent, count: 0, start: 0 } })
}
#[derive(Clone, Debug, PartialEq, Eq)]
struct Node { key: u128, priority: u64, size: usize, block: Arc<Block>, left: Link, right: Link, run: Option<Run> }
fn size(link: &Link) -> usize { link.as_ref().map_or(0, |n| n.size) }
fn update(node: &mut Node) {
    node.size = 1 + size(&node.left) + size(&node.right);
    node.run = single_run(&node.block).and_then(|mut r| {
        if let Some(left) = &node.left { r = merge_run(left.run?, r)?; }
        if let Some(right) = &node.right { r = merge_run(r, right.run?)?; }
        Some(r)
    });
}
fn priority(mut id: u64) -> u64 {
    id = id.wrapping_add(0x9e3779b97f4a7c15);
    id = (id ^ (id >> 30)).wrapping_mul(0xbf58476d1ce4e5b9);
    id = (id ^ (id >> 27)).wrapping_mul(0x94d049bb133111eb);
    id ^ (id >> 31)
}
fn join(a: Link, b: Link) -> Link {
    match (a, b) {
        (None, b) => b, (a, None) => a,
        (Some(mut a), Some(mut b)) => {
            if a.priority <= b.priority {
                let n = Arc::make_mut(&mut a); n.right = join(n.right.take(), Some(b)); update(n); Some(a)
            } else {
                let n = Arc::make_mut(&mut b); n.left = join(Some(a), n.left.take()); update(n); Some(b)
            }
        }
    }
}
fn split(root: Link, count: usize) -> (Link, Link) {
    let Some(mut root) = root else { return (None, None) };
    let n = Arc::make_mut(&mut root);
    if count <= size(&n.left) {
        let (a, b) = split(n.left.take(), count); n.left = b; update(n); (a, Some(root))
    } else {
        let (a, b) = split(n.right.take(), count - size(&n.left) - 1); n.right = a; update(n); (Some(root), b)
    }
}

/// Persistent indexed sequence. Structural changes go through Doc; block IDs are immutable.
#[derive(Clone, Debug, Default)]
pub struct Blocks { root: Link, ids: im::HashMap<u64, u128> }
impl PartialEq for Blocks {
    fn eq(&self,other:&Self)->bool {
        fn equal(a:&Link,b:&Link)->bool {
            match (a,b) {
                (None,None)=>true,
                (Some(a),Some(b))=>Arc::ptr_eq(a,b) || (a.size==b.size && a.block==b.block
                    && equal(&a.left,&b.left) && equal(&a.right,&b.right)),
                _=>false,
            }
        }
        // Treap priority depends only on immutable IDs, so an equal order has equal shape.
        equal(&self.root,&other.root)
    }
}
impl Eq for Blocks {}
impl Blocks {
    pub fn len(&self) -> usize { size(&self.root) }
    pub fn is_empty(&self) -> bool { self.root.is_none() }
    pub fn iter(&self) -> Iter<'_> { let mut i = Iter { stack: Vec::new(), remaining: self.len() }; i.descend(self.root.as_deref()); i }
    pub fn get(&self, mut index: usize) -> Option<&Block> {
        let mut node = self.root.as_deref();
        while let Some(n) = node {
            let left = size(&n.left);
            if index == left { return Some(&n.block) }
            if index < left { node = n.left.as_deref(); }
            else { index -= left + 1; node = n.right.as_deref(); }
        }
        None
    }
    pub fn first(&self) -> Option<&Block> { self.get(0) }
    pub fn last(&self) -> Option<&Block> { self.len().checked_sub(1).and_then(|i| self.get(i)) }
    pub fn index_of(&self, id: u64) -> Option<usize> {
        let key = *self.ids.get(&id)?;
        let (mut node, mut rank) = (self.root.as_deref(), 0);
        while let Some(n) = node {
            if key == n.key { return Some(rank + size(&n.left)); }
            if key < n.key { node = n.left.as_deref(); }
            else { rank += size(&n.left) + 1; node = n.right.as_deref(); }
        }
        None
    }
    pub fn by_id(&self, id: u64) -> Option<&Block> {
        let key = *self.ids.get(&id)?; let mut node = self.root.as_deref();
        while let Some(n) = node {
            if key == n.key { return Some(&n.block) }
            node = if key < n.key { n.left.as_deref() } else { n.right.as_deref() };
        }
        None
    }
    pub(super) fn by_id_mut(&mut self, id: u64) -> Option<&mut Block> {
        let key = *self.ids.get(&id)?; let mut node = self.root.as_mut();
        while let Some(arc) = node {
            let n = Arc::make_mut(arc); n.run = None;
            if key == n.key { return Some(Arc::make_mut(&mut n.block)); }
            node = if key < n.key { n.left.as_mut() } else { n.right.as_mut() };
        }
        None
    }
    pub(super) fn refresh(&mut self, id:u64) {
        let Some(&key)=self.ids.get(&id) else { return };
        fn visit(root:&mut Link,key:u128) {
            if let Some(root)=root { let n=Arc::make_mut(root);
                if key<n.key {visit(&mut n.left,key)} else if key>n.key {visit(&mut n.right,key)}
                update(n);
            }
        }
        visit(&mut self.root,key);
    }
    pub fn list_number(&self,id:u64)->Option<u64> {
        let block=self.by_id(id)?;
        if block.kind!=Kind::Numbered {return None;}
        let key=self.ids[&id]; let indent=block.meta.indent;
        let mut state=(1u64,block.meta.list_start.unwrap_or(1));
        fn scan(root:&Link,key:u128,full:bool,indent:usize,state:&mut (u64,u64))->bool {
            let Some(n)=root else {return false;};
            if full && let Some(run)=n.run {
                if run.indent>indent {return false;}
                if run.indent==indent && run.count>0 {
                    state.0=state.0.saturating_add(run.count);state.1=run.start;return false;
                }
            }
            if !full && n.key>=key {return scan(&n.left,key,false,indent,state);}
            if scan(&n.right,key,full,indent,state) {return true;}
            let b=&n.block;
            if let Some(code)=&b.meta.list_code {
                if code.parent_indent>=indent {return scan(&n.left,key,true,indent,state);}
                return true;
            }
            if !matches!(b.kind,Kind::Numbered|Kind::Bullet|Kind::Todo) || b.meta.indent<indent {return true;}
            if b.meta.indent==indent {
                if b.kind!=Kind::Numbered {return true;}
                state.0=state.0.saturating_add(1);state.1=b.meta.list_start.unwrap_or(1);
            }
            scan(&n.left,key,true,indent,state)
        }
        scan(&self.root,key,false,indent,&mut state); Some(state.1.saturating_add(state.0-1))
    }
    pub(super) fn insert(&mut self, index: usize, block: Block) {
        assert!(index <= self.len() && !self.ids.contains_key(&block.id));
        let low = index.checked_sub(1).and_then(|i| self.get(i)).map_or(0, |b| self.ids[&b.id]);
        let high = self.get(index).map_or(u128::MAX, |b| self.ids[&b.id]);
        if high - low <= 1 {
            self.relabel_near(index);
            return self.insert(index, block);
        }
        let key = if index == self.len() { low + (1u128 << 64).min((high-low)/2) }
            else { low + (high-low)/2 };
        self.ids.insert(block.id, key);
        let node = Some(Arc::new(Node { key, priority: priority(block.id), size: 1,
            run: single_run(&block),
            block: Arc::new(block), left: None, right: None }));
        let (a, b) = split(self.root.take(), index);
        self.root = join(join(a, node), b);
    }
    fn relabel_near(&mut self, index: usize) {
        let mut radius = 32;
        let (start,end,low,step) = loop {
            let start = index.saturating_sub(radius); let end = (index+radius).min(self.len());
            let low = start.checked_sub(1).and_then(|i| self.get(i)).map_or(0,|b| self.ids[&b.id]);
            let high = self.get(end).map_or(u128::MAX,|b| self.ids[&b.id]);
            let step = (high-low)/(end-start+1) as u128;
            if step >= 1u128<<32 || (start==0 && end==self.len()) { break (start,end,low,step); }
            radius *= 2;
        };
        let (a,rest) = split(self.root.take(),start); let (mut middle,b) = split(rest,end-start);
        fn assign(root:&mut Link, next:&mut u128, step:u128, ids:&mut im::HashMap<u64,u128>) {
            if let Some(root)=root { let n=Arc::make_mut(root); assign(&mut n.left,next,step,ids);
                *next+=step; n.key=*next; ids.insert(n.block.id,*next); assign(&mut n.right,next,step,ids); }
        }
        let mut next = low; assign(&mut middle,&mut next,step,&mut self.ids);
        self.root=join(join(a,middle),b);
    }
    pub(super) fn remove(&mut self, index: usize) -> Block {
        let (a, rest) = split(self.root.take(), index);
        let (item, b) = split(rest, 1);
        self.root = join(a, b);
        let item = item.unwrap(); self.ids.remove(&item.block.id);
        Arc::try_unwrap(item).map(|n| Arc::unwrap_or_clone(n.block)).unwrap_or_else(|n| (*n.block).clone())
    }
}
impl Index<usize> for Blocks {
    type Output = Block;
    fn index(&self, index: usize) -> &Block { self.get(index).expect("block index out of bounds") }
}
pub struct Iter<'a> { stack: Vec<&'a Node>, remaining: usize }
impl<'a> Iter<'a> {
    fn descend(&mut self, mut node: Option<&'a Node>) {
        while let Some(n) = node { self.stack.push(n); node = n.left.as_deref(); }
    }
}
impl<'a> Iterator for Iter<'a> {
    type Item = &'a Block;
    fn next(&mut self) -> Option<Self::Item> {
        let n = self.stack.pop()?; self.descend(n.right.as_deref()); self.remaining -= 1; Some(&n.block)
    }
    fn size_hint(&self) -> (usize, Option<usize>) { (self.remaining, Some(self.remaining)) }
}
impl ExactSizeIterator for Iter<'_> {}
impl<'a> IntoIterator for &'a Blocks {
    type Item = &'a Block; type IntoIter = Iter<'a>;
    fn into_iter(self) -> Self::IntoIter { self.iter() }
}
impl FromIterator<Block> for Blocks {
    fn from_iter<T: IntoIterator<Item = Block>>(iter: T) -> Self {
        // Cartesian-tree stack builds the ordered treap in linear time.
        let mut result = Self::default(); let mut stack: Vec<Node> = Vec::new();
        for (i, block) in iter.into_iter().enumerate() {
            let key = (i as u128 + 1) << 64; result.ids.insert(block.id, key);
            let mut node = Node { key, priority: priority(block.id), size: 1,
                run: single_run(&block),
            block: Arc::new(block), left: None, right: None };
            let mut left = None;
            while stack.last().is_some_and(|n| n.priority > node.priority) {
                let mut n = stack.pop().unwrap(); n.right = left; update(&mut n); left = Some(Arc::new(n));
            }
            node.left = left; stack.push(node);
        }
        let mut root = None;
        while let Some(mut n) = stack.pop() { n.right = root; update(&mut n); root = Some(Arc::new(n)); }
        result.root = root; result
    }
}
impl Serialize for Blocks {
    fn serialize<S: Serializer>(&self, serializer: S) -> Result<S::Ok, S::Error> {
        serializer.collect_seq(self.iter())
    }
}
impl<'de> Deserialize<'de> for Blocks {
    fn deserialize<D: Deserializer<'de>>(deserializer: D) -> Result<Self, D::Error> {
        Vec::<Block>::deserialize(deserializer).map(|b| b.into_iter().collect())
    }
}
