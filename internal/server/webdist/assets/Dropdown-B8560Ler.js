import{cr as $e,cs as Oe,O as Be,ch as Z,ct as Ae,v as Fe,bM as Q,K as fe,by as he,d as F,o as a,c as w,ad as N,s as B,n as g,Z as X,_ as k,Y as te,c5 as ne,F as _,cu as be,P as A,bL as oe,z as y,ax as Te,aG as ge,S as W,a as ee,ck as Me,cv as Ee,aH as je,aF as Le,W as R,cm as He,V,bv as me,ak as K,aJ as z,X as xe,cw as Ue,$ as Ve,cx as We,am as qe,aP as ce,aS as I,a1 as O}from"./index-CmpWsR0j.js";import{c as Ge}from"./create-ref-setter-C4J8sofl.js";import{V as Xe,d as Je,B as Ye,r as Ze,p as ke,P as Qe}from"./Popover-DE1bWV4E.js";import{C as eo}from"./ChevronRight-FK7PlGWV.js";import{N as oo}from"./Icon-D1y76Zg8.js";import{h as we,c as no}from"./create-DLefnRiD.js";import{u as to}from"./Input-Co-6TWYz.js";function ro(e={},n){const i=Fe({ctrl:!1,command:!1,win:!1,shift:!1,tab:!1}),{keydown:t,keyup:r}=e,d=l=>{switch(l.key){case"Control":i.ctrl=!0;break;case"Meta":i.command=!0,i.win=!0;break;case"Shift":i.shift=!0;break;case"Tab":i.tab=!0;break}t!==void 0&&Object.keys(t).forEach(S=>{if(S!==l.key)return;const m=t[S];if(typeof m=="function")m(l);else{const{stop:P=!1,prevent:x=!1}=m;P&&l.stopPropagation(),x&&l.preventDefault(),m.handler(l)}})},s=l=>{switch(l.key){case"Control":i.ctrl=!1;break;case"Meta":i.command=!1,i.win=!1;break;case"Shift":i.shift=!1;break;case"Tab":i.tab=!1;break}r!==void 0&&Object.keys(r).forEach(S=>{if(S!==l.key)return;const m=r[S];if(typeof m=="function")m(l);else{const{stop:P=!1,prevent:x=!1}=m;P&&l.stopPropagation(),x&&l.preventDefault(),m.handler(l)}})},c=()=>{(n===void 0||n.value)&&(Q("keydown",document,d),Q("keyup",document,s)),n!==void 0&&fe(n,l=>{l?(Q("keydown",document,d),Q("keyup",document,s)):(Z("keydown",document,d),Z("keyup",document,s))})};return $e()?(Oe(c),Be(()=>{(n===void 0||n.value)&&(Z("keydown",document,d),Z("keyup",document,s))})):c(),Ae(i)}const ve=he("n-dropdown-menu"),re=he("n-dropdown"),ye=he("n-dropdown-option");var Se=F({name:"DropdownDivider",props:{clsPrefix:{type:String,required:!0}},render(){return a(),w("div",{class:N(`${this.clsPrefix}-dropdown-divider`)},null,2)}});function pe(e,n){return e.type==="submenu"||e.type===void 0&&e[n]!==void 0}function io(e){return e.type==="group"}function Pe(e){return e.type==="divider"}function ao(e){return e.type==="render"}function lo(e,n,i){const t=B(e.value);let r=null;return fe(e,d=>{r!==null&&window.clearTimeout(r),d===!0?i&&!i.value?t.value=!0:r=window.setTimeout(()=>{t.value=!0},n):t.value=!1}),t}var Ne=F({name:"DropdownOption",props:{clsPrefix:{type:String,required:!0},tmNode:{type:Object,required:!0},parentKey:{type:[String,Number],default:null},placement:{type:String,default:"right-start"},props:Object,scrollable:Boolean},setup(e){const n=A(re),{hoverKeyRef:i,keyboardKeyRef:t,lastToggledSubmenuKeyRef:r,pendingKeyPathRef:d,activeKeyPathRef:s,animatedRef:c,mergedShowRef:l,renderLabelRef:S,renderIconRef:m,labelFieldRef:P,childrenFieldRef:x,renderOptionRef:T,nodePropsRef:M,menuPropsRef:C}=n,E=A(ye,null),j=A(ve),L=A(ge),J=y(()=>e.tmNode.rawNode),q=y(()=>{const{value:o}=x;return pe(e.tmNode.rawNode,o)}),ie=y(()=>{const{disabled:o}=e.tmNode;return o}),de=y(()=>{if(!q.value)return!1;const{key:o,disabled:u}=e.tmNode;if(u)return!1;const{value:b}=i,{value:D}=t,{value:ue}=r,{value:$}=d;return b!==null?$.includes(o):D!==null?$.includes(o)&&$[$.length-1]!==o:ue!==null?$.includes(o):!1}),ae=y(()=>t.value===null&&!c.value),le=lo(de,300,ae),se=y(()=>!!E?.enteringSubmenuRef.value),H=B(!1);W(ye,{enteringSubmenuRef:H});function U(){H.value=!0}function Y(){H.value=!1}function G(){const{parentKey:o,tmNode:u}=e;u.disabled||l.value&&(r.value=o,t.value=null,i.value=u.key)}function p(){const{tmNode:o}=e;o.disabled||l.value&&i.value!==o.key&&G()}function f(o){if(e.tmNode.disabled||!l.value)return;const{relatedTarget:u}=o;u&&!we({target:u},"dropdownOption")&&!we({target:u},"scrollbarRail")&&(i.value=null)}function v(){const{value:o}=q,{tmNode:u}=e;l.value&&!o&&!u.disabled&&(n.doSelect(u.key,u.rawNode),n.doUpdateShow(!1))}return{labelField:P,renderLabel:S,renderIcon:m,siblingHasIcon:j.showIconRef,siblingHasSubmenu:j.hasSubmenuRef,menuProps:C,popoverBody:L,animated:c,mergedShowSubmenu:y(()=>le.value&&!se.value),rawNode:J,hasSubmenu:q,pending:oe(()=>{const{value:o}=d,{key:u}=e.tmNode;return o.includes(u)}),childActive:oe(()=>{const{value:o}=s,{key:u}=e.tmNode,b=o.findIndex(D=>u===D);return b===-1?!1:b<o.length-1}),active:oe(()=>{const{value:o}=s,{key:u}=e.tmNode,b=o.findIndex(D=>u===D);return b===-1?!1:b===o.length-1}),mergedDisabled:ie,renderOption:T,nodeProps:M,handleClick:v,handleMouseMove:p,handleMouseEnter:G,handleMouseLeave:f,handleSubmenuBeforeEnter:U,handleSubmenuAfterEnter:Y}},render(){const{animated:e,rawNode:n,mergedShowSubmenu:i,clsPrefix:t,siblingHasIcon:r,siblingHasSubmenu:d,renderLabel:s,renderIcon:c,renderOption:l,nodeProps:S,props:m,scrollable:P}=this;let x=null;if(i){const E=this.menuProps?.(n,n.children);x=(j=>(a(),g(Re,X({key:1},E,{clsPrefix:t,scrollable:this.scrollable,tmNodes:this.tmNode.children,parentKey:this.tmNode.key}),null,16,["clsPrefix","scrollable","tmNodes","parentKey"])))()}const T={class:[`${t}-dropdown-option-body`,this.pending&&`${t}-dropdown-option-body--pending`,this.active&&`${t}-dropdown-option-body--active`,this.childActive&&`${t}-dropdown-option-body--child-active`,this.mergedDisabled&&`${t}-dropdown-option-body--disabled`],onMousemove:this.handleMouseMove,onMouseenter:this.handleMouseEnter,onMouseleave:this.handleMouseLeave,onClick:this.handleClick},M=S?.(n),C=(a(),w("div",X({class:[`${t}-dropdown-option`,M?.class],"data-dropdown-option":!0},M),[k(()=>te("div",X(T,m),[(a(),w("div",{class:N([`${t}-dropdown-option-body__prefix`,r&&`${t}-dropdown-option-body__prefix--show-icon`])},[k(()=>[c?c(n):ne(n.icon)])],2)),(a(),w("div",{"data-dropdown-option":!0,class:N(`${t}-dropdown-option-body__label`)},[s?(a(),w(_,{key:0},[k(()=>s(n))],64)):(a(),w(_,{key:1},[k(()=>ne(n[this.labelField]??n.title))],64))],2)),(a(),w("div",{"data-dropdown-option":!0,class:N([`${t}-dropdown-option-body__suffix`,d&&`${t}-dropdown-option-body__suffix--has-submenu`])},[this.hasSubmenu?(a(),g(oo,{key:0},{_:1,default:be(()=>(a(),g(eo)))})):k(()=>null)],2))])),this.hasSubmenu?(a(),g(Ye,{key:0},{default:()=>[(a(),g(Xe,null,{default:()=>(a(),w("div",{class:N(`${t}-dropdown-offset-container`)},[(a(),g(Je,{show:this.mergedShowSubmenu,placement:this.placement,to:P&&this.popoverBody||void 0,teleportDisabled:!P},{default:()=>(a(),w("div",{class:N(`${t}-dropdown-menu-wrapper`)},[e?(a(),g(Te,{key:0,onBeforeEnter:this.handleSubmenuBeforeEnter,onAfterEnter:this.handleSubmenuAfterEnter,name:"fade-in-scale-up-transition",appear:!0},{default:()=>x},1032,["onBeforeEnter","onAfterEnter"])):(a(),w(_,{key:1},[k(()=>x)],64))],2))},1032,["show","placement","to","teleportDisabled"]))],2))},1024))]},1024)):k(()=>null)],16));return l?l({node:C,option:n}):C}}),so=F({name:"DropdownGroupHeader",props:{clsPrefix:{type:String,required:!0},tmNode:{type:Object,required:!0}},setup(){const{showIconRef:e,hasSubmenuRef:n}=A(ve),{renderLabelRef:i,labelFieldRef:t,nodePropsRef:r,renderOptionRef:d}=A(re);return{labelField:t,showIcon:e,hasSubmenu:n,renderLabel:i,nodeProps:r,renderOption:d}},render(){const{clsPrefix:e,hasSubmenu:n,showIcon:i,nodeProps:t,renderLabel:r,renderOption:d}=this,{rawNode:s}=this.tmNode,c=(a(),w("div",X({class:`${e}-dropdown-option`},t?.(s)),[ee("div",{class:N(`${e}-dropdown-option-body ${e}-dropdown-option-body--group`)},[ee("div",{"data-dropdown-option":!0,class:N([`${e}-dropdown-option-body__prefix`,i&&`${e}-dropdown-option-body__prefix--show-icon`])},[k(()=>ne(s.icon))],2),ee("div",{class:N(`${e}-dropdown-option-body__label`),"data-dropdown-option":!0},[r?(a(),w(_,{key:0},[k(()=>r(s))],64)):(a(),w(_,{key:1},[k(()=>ne(s.title??s[this.labelField]))],64))],2),ee("div",{class:N([`${e}-dropdown-option-body__suffix`,n&&`${e}-dropdown-option-body__suffix--has-submenu`]),"data-dropdown-option":!0},null,2)],2)],16));return d?d({node:c,option:s}):c}}),uo=F({name:"NDropdownGroup",props:{clsPrefix:{type:String,required:!0},tmNode:{type:Object,required:!0},parentKey:{type:[String,Number],default:null}},render(){const{tmNode:e,parentKey:n,clsPrefix:i}=this,{children:t}=e;return a(),w(_,null,[(a(),g(so,{clsPrefix:i,tmNode:e,key:e.key},null,8,["clsPrefix","tmNode"])),k(()=>t?.map(r=>{const{rawNode:d}=r;return d.show===!1?null:Pe(d)?te(Se,{clsPrefix:i,key:r.key}):r.isGroup?(Me("dropdown","`group` node is not allowed to be put in `group` node."),null):(a(),g(Ne,{clsPrefix:i,tmNode:r,parentKey:n,key:r.key},null,8,["clsPrefix","tmNode","parentKey"]))}))],64)}}),co=F({name:"DropdownRenderOption",props:{tmNode:{type:Object,required:!0}},render(){const{rawNode:{render:e,props:n}}=this.tmNode;return te("div",n,[e?.()])}}),Re=F({name:"DropdownMenu",props:{scrollable:Boolean,showArrow:Boolean,arrowStyle:[String,Object],clsPrefix:{type:String,required:!0},tmNodes:{type:Array,default:()=>[]},parentKey:{type:[String,Number],default:null}},setup(e){const{renderIconRef:n,childrenFieldRef:i}=A(re);W(ve,{showIconRef:y(()=>{const r=n.value;return e.tmNodes.some(d=>{if(d.isGroup)return d.children?.some(({rawNode:c})=>r?r(c):c.icon);const{rawNode:s}=d;return r?r(s):s.icon})}),hasSubmenuRef:y(()=>{const{value:r}=i;return e.tmNodes.some(d=>{if(d.isGroup)return d.children?.some(({rawNode:c})=>pe(c,r));const{rawNode:s}=d;return pe(s,r)})})});const t=B(null);return W(je,null),W(Le,null),W(ge,t),{bodyRef:t}},render(){const{parentKey:e,clsPrefix:n,scrollable:i}=this,t=this.tmNodes.map(r=>{const{rawNode:d}=r;return d.show===!1?null:ao(d)?(a(),g(co,{tmNode:r,key:r.key},null,8,["tmNode"])):Pe(d)?(a(),g(Se,{clsPrefix:n,key:r.key},null,8,["clsPrefix"])):io(d)?(a(),g(uo,{clsPrefix:n,tmNode:r,parentKey:e,key:r.key},null,8,["clsPrefix","tmNode","parentKey"])):(a(),g(Ne,{clsPrefix:n,tmNode:r,parentKey:e,key:r.key,props:d.props,scrollable:i},null,8,["clsPrefix","tmNode","parentKey","props","scrollable"]))});return a(),w("div",{class:N([`${n}-dropdown-menu`,i&&`${n}-dropdown-menu--scrollable`]),ref:"bodyRef"},[i?(a(),g(Ee,{key:0,contentClass:`${n}-dropdown-menu__content`},{default:()=>t},1032,["contentClass"])):(a(),w(_,{key:1},[k(()=>t)],64)),this.showArrow?(a(),w(_,{key:2},[k(()=>Ze({clsPrefix:n,arrowStyle:this.arrowStyle,arrowClass:void 0,arrowWrapperClass:void 0,arrowWrapperStyle:void 0}))],64)):k(()=>null)],2)}}),po=R("dropdown-menu",`
 transform-origin: var(--v-transform-origin);
 background-color: var(--n-color);
 border-radius: var(--n-border-radius);
 box-shadow: var(--n-box-shadow);
 position: relative;
 transition:
 background-color .3s var(--n-bezier),
 box-shadow .3s var(--n-bezier);
`,[He(),R("dropdown-option",`
 position: relative;
 `,[V("a",`
 text-decoration: none;
 color: inherit;
 outline: none;
 `,[V("&::before",`
 content: "";
 position: absolute;
 left: 0;
 right: 0;
 top: 0;
 bottom: 0;
 `)]),R("dropdown-option-body",`
 display: flex;
 cursor: pointer;
 position: relative;
 height: var(--n-option-height);
 line-height: var(--n-option-height);
 font-size: var(--n-font-size);
 color: var(--n-option-text-color);
 transition: color .3s var(--n-bezier);
 `,[V("&::before",`
 content: "";
 position: absolute;
 top: 0;
 bottom: 0;
 left: 4px;
 right: 4px;
 transition: background-color .3s var(--n-bezier);
 border-radius: var(--n-border-radius);
 `),me("disabled",[K("pending",`
 color: var(--n-option-text-color-hover);
 `,[z("prefix, suffix",`
 color: var(--n-option-text-color-hover);
 `),V("&::before","background-color: var(--n-option-color-hover);")]),K("active",`
 color: var(--n-option-text-color-active);
 `,[z("prefix, suffix",`
 color: var(--n-option-text-color-active);
 `),V("&::before","background-color: var(--n-option-color-active);")]),K("child-active",`
 color: var(--n-option-text-color-child-active);
 `,[z("prefix, suffix",`
 color: var(--n-option-text-color-child-active);
 `)])]),K("disabled",`
 cursor: not-allowed;
 opacity: var(--n-option-opacity-disabled);
 `),K("group",`
 font-size: calc(var(--n-font-size) - 1px);
 color: var(--n-group-header-text-color);
 `,[z("prefix",`
 width: calc(var(--n-option-prefix-width) / 2);
 `,[K("show-icon",`
 width: calc(var(--n-option-icon-prefix-width) / 2);
 `)])]),z("prefix",`
 width: var(--n-option-prefix-width);
 display: flex;
 justify-content: center;
 align-items: center;
 color: var(--n-prefix-color);
 transition: color .3s var(--n-bezier);
 z-index: 1;
 `,[K("show-icon",`
 width: var(--n-option-icon-prefix-width);
 `),R("icon",`
 font-size: var(--n-option-icon-size);
 `)]),z("label",`
 white-space: nowrap;
 flex: 1;
 z-index: 1;
 `),z("suffix",`
 box-sizing: border-box;
 flex-grow: 0;
 flex-shrink: 0;
 display: flex;
 justify-content: flex-end;
 align-items: center;
 min-width: var(--n-option-suffix-width);
 padding: 0 8px;
 transition: color .3s var(--n-bezier);
 color: var(--n-suffix-color);
 z-index: 1;
 `,[K("has-submenu",`
 width: var(--n-option-icon-suffix-width);
 `),R("icon",`
 font-size: var(--n-option-icon-size);
 `)]),R("dropdown-menu","pointer-events: all;")]),R("dropdown-offset-container",`
 pointer-events: none;
 position: absolute;
 left: 0;
 right: 0;
 top: -4px;
 bottom: -4px;
 `)]),R("dropdown-divider",`
 transition: background-color .3s var(--n-bezier);
 background-color: var(--n-divider-color);
 height: 1px;
 margin: 4px 0;
 `),R("dropdown-menu-wrapper",`
 transform-origin: var(--v-transform-origin);
 width: fit-content;
 `),V(">",[R("scrollbar",`
 height: inherit;
 max-height: inherit;
 `)]),me("scrollable",`
 padding: var(--n-padding);
 `),K("scrollable",[z("content",`
 padding: var(--n-padding);
 `)])]);const fo={animated:{type:Boolean,default:!0},keyboard:{type:Boolean,default:!0},size:String,inverted:Boolean,placement:{type:String,default:"bottom"},onSelect:[Function,Array],options:{type:Array,default:()=>[]},menuProps:Function,showArrow:Boolean,renderLabel:Function,renderIcon:Function,renderOption:Function,nodeProps:Function,labelField:{type:String,default:"label"},keyField:{type:String,default:"key"},childrenField:{type:String,default:"children"},value:[String,Number]},ho=Object.keys(ke),vo={...ke,...fo,...xe.props};var So=F({name:"Dropdown",inheritAttrs:!1,props:vo,setup(e){const n=B(!1),i=to(I(e,"show"),n),t=y(()=>{const{keyField:p,childrenField:f}=e;return no(e.options,{getKey(v){return v[p]},getDisabled(v){return v.disabled===!0},getIgnored(v){return v.type==="divider"||v.type==="render"},getChildren(v){return v[f]}})}),r=y(()=>t.value.treeNodes),d=B(null),s=B(null),c=B(null),l=y(()=>d.value??s.value??c.value??null),S=y(()=>t.value.getPath(l.value).keyPath),m=y(()=>t.value.getPath(e.value).keyPath),P=oe(()=>e.keyboard&&i.value);ro({keydown:{ArrowUp:{prevent:!0,handler:ae},ArrowRight:{prevent:!0,handler:de},ArrowDown:{prevent:!0,handler:le},ArrowLeft:{prevent:!0,handler:ie},Enter:{prevent:!0,handler:se},Escape:q}},P);const{mergedClsPrefixRef:x,inlineThemeDisabled:T,mergedComponentPropsRef:M}=Ve(e),C=y(()=>e.size||M?.value?.Dropdown?.size||"medium"),E=xe("Dropdown","-dropdown",po,We,e,x);W(re,{labelFieldRef:I(e,"labelField"),childrenFieldRef:I(e,"childrenField"),renderLabelRef:I(e,"renderLabel"),renderIconRef:I(e,"renderIcon"),hoverKeyRef:d,keyboardKeyRef:s,lastToggledSubmenuKeyRef:c,pendingKeyPathRef:S,activeKeyPathRef:m,animatedRef:I(e,"animated"),mergedShowRef:i,nodePropsRef:I(e,"nodeProps"),renderOptionRef:I(e,"renderOption"),menuPropsRef:I(e,"menuProps"),doSelect:j,doUpdateShow:L}),fe(i,p=>{!e.animated&&!p&&J()});function j(p,f){const{onSelect:v}=e;v&&ce(v,p,f)}function L(p){const{"onUpdate:show":f,onUpdateShow:v}=e;f&&ce(f,p),v&&ce(v,p),n.value=p}function J(){d.value=null,s.value=null,c.value=null}function q(){L(!1)}function ie(){U("left")}function de(){U("right")}function ae(){U("up")}function le(){U("down")}function se(){const p=H();p?.isLeaf&&i.value&&(j(p.key,p.rawNode),L(!1))}function H(){const{value:p}=t,{value:f}=l;return!p||f===null?null:p.getNode(f)??null}function U(p){const{value:f}=l,{value:{getFirstAvailableNode:v}}=t;let o=null;if(f===null){const u=v();u!==null&&(o=u.key)}else{const u=H();if(u){let b;switch(p){case"down":b=u.getNext();break;case"up":b=u.getPrev();break;case"right":b=u.getChild();break;case"left":b=u.getParent()}b&&(o=b.key)}}o!==null&&(d.value=null,s.value=o)}const Y=y(()=>{const{inverted:p}=e,f=C.value,{common:{cubicBezierEaseInOut:v},self:o}=E.value,{padding:u,dividerColor:b,borderRadius:D,optionOpacityDisabled:ue,[O("optionIconSuffixWidth",f)]:$,[O("optionSuffixWidth",f)]:Ke,[O("optionIconPrefixWidth",f)]:Ie,[O("optionPrefixWidth",f)]:Ce,[O("fontSize",f)]:ze,[O("optionHeight",f)]:_e,[O("optionIconSize",f)]:De}=o,h={"--n-bezier":v,"--n-font-size":ze,"--n-padding":u,"--n-border-radius":D,"--n-option-height":_e,"--n-option-prefix-width":Ce,"--n-option-icon-prefix-width":Ie,"--n-option-suffix-width":Ke,"--n-option-icon-suffix-width":$,"--n-option-icon-size":De,"--n-divider-color":b,"--n-option-opacity-disabled":ue};return p?(h["--n-color"]=o.colorInverted,h["--n-option-color-hover"]=o.optionColorHoverInverted,h["--n-option-color-active"]=o.optionColorActiveInverted,h["--n-option-text-color"]=o.optionTextColorInverted,h["--n-option-text-color-hover"]=o.optionTextColorHoverInverted,h["--n-option-text-color-active"]=o.optionTextColorActiveInverted,h["--n-option-text-color-child-active"]=o.optionTextColorChildActiveInverted,h["--n-prefix-color"]=o.prefixColorInverted,h["--n-suffix-color"]=o.suffixColorInverted,h["--n-group-header-text-color"]=o.groupHeaderTextColorInverted):(h["--n-color"]=o.color,h["--n-option-color-hover"]=o.optionColorHover,h["--n-option-color-active"]=o.optionColorActive,h["--n-option-text-color"]=o.optionTextColor,h["--n-option-text-color-hover"]=o.optionTextColorHover,h["--n-option-text-color-active"]=o.optionTextColorActive,h["--n-option-text-color-child-active"]=o.optionTextColorChildActive,h["--n-prefix-color"]=o.prefixColor,h["--n-suffix-color"]=o.suffixColor,h["--n-group-header-text-color"]=o.groupHeaderTextColor),h}),G=T?qe("dropdown",y(()=>`${C.value[0]}${e.inverted?"i":""}`),Y,e):void 0;return{mergedClsPrefix:x,mergedTheme:E,mergedSize:C,tmNodes:r,mergedShow:i,handleAfterLeave:()=>{e.animated&&J()},doUpdateShow:L,cssVars:T?void 0:Y,themeClass:G?.themeClass,onRender:G?.onRender}},render(){const e=(t,r,d,s,c)=>{const{mergedClsPrefix:l,menuProps:S}=this;this.onRender?.();const m=S?.(void 0,this.tmNodes.map(x=>x.rawNode))||{},P={ref:Ge(r),class:[t,`${l}-dropdown`,`${l}-dropdown--${this.mergedSize}-size`,this.themeClass],clsPrefix:l,tmNodes:this.tmNodes,style:[...d,this.cssVars],showArrow:this.showArrow,arrowStyle:this.arrowStyle,scrollable:this.scrollable,onMouseenter:s,onMouseleave:c};return te(Re,X(this.$attrs,P,m))},{mergedTheme:n}=this,i={show:this.mergedShow,theme:n.peers.Popover,themeOverrides:n.peerOverrides.Popover,internalOnAfterLeave:this.handleAfterLeave,internalRenderBody:e,onUpdateShow:this.doUpdateShow,"onUpdate:show":void 0};return a(),g(Qe,Ue(this.$props,ho,i),{_:1,trigger:be(()=>this.$slots.default?.())},16)}});export{So as D};
