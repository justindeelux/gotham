import{U as co,V as le,A as z,W as uo,z as w,X as vo,Y as y,d as j,H as fe,Z as mo,_ as ho,o as v,c as S,I as _,K as me,J as B,M as Be,$ as G,N as Ve,a as C,s as V,h as je,O as we,a0 as fo,a1 as Ae,m as O,a2 as De,a3 as ae,a4 as po,S as go,a5 as bo,E,a6 as ne,a7 as xo,a8 as _e,b as h,a9 as oe,F as U,aa as Co,ab as Pe,ac as te,ad as Ie,ae as he,af as yo,ag as ge,ah as zo,ai as W,aj as wo,ak as Oe,i as Ue,al as Io,f as P,u as R,w as k,k as Q,y as Se,r as So,t as X,l as ie,n as Ge,am as Ro,g as ko,B as be,R as Ao,j as _o}from"./index-CTztV8lM.js";import{t as Po,D as qe,V as No,c as xe,u as Ho}from"./servers-BYdlhYua.js";import{u as Mo,t as Fe,S as $e}from"./text-BRypjGdr.js";import{T as ee}from"./Tooltip-eT3ZFqzv.js";import{u as Le}from"./format-length-BWKcxfg3.js";import{_ as We}from"./_plugin-vue_export-helper-DlAUqK2U.js";const Eo=co&&"loading"in document.createElement("img");function Oo(e={}){const{root:n=null}=e;return{hash:`${e.rootMargin||"0px 0px 0px 0px"}-${Array.isArray(e.threshold)?e.threshold.join(","):e.threshold??"0"}`,options:{...e,root:(typeof n=="string"?document.querySelector(n):n)||document.documentElement}}}const Ce=new WeakMap,ye=new WeakMap,ze=new WeakMap,Fo=(e,n,r)=>{if(!e)return()=>{};const l=Oo(n),{root:s}=l.options;let a;const d=Ce.get(s);d?a=d:(a=new Map,Ce.set(s,a));let u,c;a.has(l.hash)?(c=a.get(l.hash),c[1].has(e)||(u=c[0],c[1].add(e),u.observe(e))):(u=new IntersectionObserver(i=>{i.forEach(m=>{if(m.isIntersecting){const N=ye.get(m.target),F=ze.get(m.target);N&&N(),F&&(F.value=!0)}})},l.options),u.observe(e),c=[u,new Set([e])],a.set(l.hash,c));let g=!1;const p=()=>{g||(ye.delete(e),ze.delete(e),g=!0,c[1].has(e)&&(c[0].unobserve(e),c[1].delete(e)),c[1].size<=0&&a.delete(l.hash),a.size||Ce.delete(s))};return ye.set(e,p),ze.set(e,r),p},$o=le("n-avatar-group");var Lo=z("avatar",`
 width: var(--n-merged-size);
 height: var(--n-merged-size);
 color: #FFF;
 font-size: var(--n-font-size);
 display: inline-flex;
 position: relative;
 overflow: hidden;
 text-align: center;
 border: var(--n-border);
 border-radius: var(--n-border-radius);
 --n-merged-color: var(--n-color);
 background-color: var(--n-merged-color);
 transition:
 border-color .3s var(--n-bezier),
 background-color .3s var(--n-bezier),
 color .3s var(--n-bezier);
`,[uo(w("&","--n-merged-color: var(--n-color-modal);")),vo(w("&","--n-merged-color: var(--n-color-popover);")),w("img",`
 width: 100%;
 height: 100%;
 `),y("text",`
 white-space: nowrap;
 display: inline-block;
 position: absolute;
 left: 50%;
 top: 50%;
 `),z("icon",`
 vertical-align: bottom;
 font-size: calc(var(--n-merged-size) - 6px);
 `),y("text","line-height: 1.25")]);const To=["src"],Ko={...fe.props,size:[String,Number],src:String,circle:{type:Boolean,default:void 0},objectFit:String,round:{type:Boolean,default:void 0},bordered:{type:Boolean,default:void 0},onError:Function,fallbackSrc:String,intersectionObserverOptions:Object,lazy:Boolean,onLoad:Function,renderPlaceholder:Function,renderFallback:Function,imgProps:Object,color:String};var Bo=j({name:"Avatar",props:Ko,slots:Object,setup(e){const{mergedClsPrefixRef:n,inlineThemeDisabled:r}=Be(e),l=V(!1);let s=null;const a=V(null),d=V(null),u=()=>{const{value:b}=a;if(b&&(s===null||s!==b.innerHTML)){s=b.innerHTML;const{value:A}=d;if(A){const{offsetWidth:L,offsetHeight:K}=A,{offsetWidth:M,offsetHeight:Y}=b,q=.9,J=Math.min(L/M*q,K/Y*q,1);b.style.transform=`translateX(-50%) translateY(-50%) scale(${J})`}}},c=G($o,null),g=C(()=>{const{size:b}=e;if(b)return b;const{size:A}=c||{};return A||"medium"}),p=fe("Avatar","-avatar",Lo,po,e,n),i=G(Po,null),m=C(()=>{if(c)return!0;const{round:b,circle:A}=e;return b!==void 0||A!==void 0?b||A:i?i.roundRef.value:!1}),N=C(()=>c?!0:e.bordered||!1),F=C(()=>{const b=g.value,A=m.value,L=N.value,{color:K}=e,{self:{borderRadius:M,fontSize:Y,color:q,border:J,colorModal:pe,colorPopover:ce},common:{cubicBezierEaseInOut:de}}=p.value;let re;return typeof b=="number"?re=`${b}px`:re=p.value.self[go("height",b)],{"--n-font-size":Y,"--n-border":L?J:"none","--n-border-radius":A?"50%":M,"--n-color":K||q,"--n-color-modal":K||pe,"--n-color-popover":K||ce,"--n-bezier":de,"--n-merged-size":`var(--n-avatar-size-override, ${re})`}}),$=r?Ve("avatar",C(()=>{const b=g.value,A=m.value,L=N.value,{color:K}=e;let M="";return b&&(typeof b=="number"?M+=`a${b}`:M+=b[0]),A&&(M+="b"),L&&(M+="c"),K&&(M+=bo(K)),M}),F,e):void 0,H=V(!e.lazy);je(()=>{if(e.lazy&&e.intersectionObserverOptions){let b;const A=we(()=>{b?.(),b=void 0,e.lazy&&(b=Fo(d.value,e.intersectionObserverOptions,H))});fo(()=>{A(),b?.()})}}),Ae(()=>e.src||e.imgProps?.src,()=>{l.value=!1});const D=V(!e.lazy);return{textRef:a,selfRef:d,mergedRoundRef:m,mergedClsPrefix:n,fitTextTransform:u,cssVars:r?void 0:F,themeClass:$?.themeClass,onRender:$?.onRender,hasLoadError:l,shouldStartLoading:H,loaded:D,mergedOnError:b=>{if(!H.value)return;l.value=!0;const{onError:A,imgProps:{onError:L}={}}=e;A?.(b),L?.(b)},mergedOnLoad:b=>{const{onLoad:A,imgProps:{onLoad:L}={}}=e;A?.(b),L?.(b),D.value=!0}}},render(){const{$slots:e,src:n,mergedClsPrefix:r,lazy:l,onRender:s,loaded:a,hasLoadError:d,imgProps:u={}}=this;s?.();let c;const g=!a&&!d&&(this.renderPlaceholder?this.renderPlaceholder():this.$slots.placeholder?.());return this.hasLoadError?c=this.renderFallback?this.renderFallback():mo(e.fallback,()=>[(v(),S("img",{src:this.fallbackSrc,style:me({objectFit:this.objectFit})},null,12,To))]):c=ho(e.default,p=>{if(p)return v(),O(De,{key:1,onResize:this.fitTextTransform},{default:()=>(v(),S("span",{ref:"textRef",class:B(`${r}-avatar__text`)},[_(()=>p)],2))},1032,["onResize"]);if(n||u.src){const i=this.src||u.src;return ae("img",{...u,loading:Eo&&!this.intersectionObserverOptions&&l?"lazy":"eager",src:l&&this.intersectionObserverOptions?this.shouldStartLoading?i:void 0:i,"data-image-src":i,onLoad:this.mergedOnLoad,onError:this.mergedOnError,style:[u.style||"",{objectFit:this.objectFit},g?{height:"0",width:"0",visibility:"hidden",position:"absolute"}:""]})}}),v(),S("span",{ref:"selfRef",class:B([`${r}-avatar`,this.themeClass]),style:me(this.cssVars)},[_(()=>c),_(()=>l&&g)],6)}});const Vo=le("n-layout-sider"),se=le("n-menu"),Ze=le("n-submenu"),Ne=le("n-menu-item-group"),Te=[w("&::before","background-color: var(--n-item-color-hover);"),y("arrow",`
 color: var(--n-arrow-color-hover);
 `),y("icon",`
 color: var(--n-item-icon-color-hover);
 `),z("menu-item-content-header",`
 color: var(--n-item-text-color-hover);
 `,[w("a",`
 color: var(--n-item-text-color-hover);
 `),y("extra",`
 color: var(--n-item-text-color-hover);
 `)])],Ke=[y("icon",`
 color: var(--n-item-icon-color-hover-horizontal);
 `),z("menu-item-content-header",`
 color: var(--n-item-text-color-hover-horizontal);
 `,[w("a",`
 color: var(--n-item-text-color-hover-horizontal);
 `),y("extra",`
 color: var(--n-item-text-color-hover-horizontal);
 `)])];var jo=w([z("menu",`
 background-color: var(--n-color);
 color: var(--n-item-text-color);
 overflow: hidden;
 transition: background-color .3s var(--n-bezier);
 box-sizing: border-box;
 font-size: var(--n-font-size);
 padding-bottom: 6px;
 `,[E("horizontal",`
 max-width: 100%;
 width: 100%;
 display: flex;
 overflow: hidden;
 padding-bottom: 0;
 `,[z("submenu","margin: 0;"),z("menu-item","margin: 0;"),z("menu-item-content",`
 padding: 0 20px;
 border-bottom: 2px solid #0000;
 `,[w("&::before","display: none;"),E("selected","border-bottom: 2px solid var(--n-border-color-horizontal)")]),z("menu-item-content",[E("selected",[y("icon","color: var(--n-item-icon-color-active-horizontal);"),z("menu-item-content-header",`
 color: var(--n-item-text-color-active-horizontal);
 `,[w("a","color: var(--n-item-text-color-active-horizontal);"),y("extra","color: var(--n-item-text-color-active-horizontal);")])]),E("child-active",`
 border-bottom: 2px solid var(--n-border-color-horizontal);
 `,[z("menu-item-content-header",`
 color: var(--n-item-text-color-child-active-horizontal);
 `,[w("a",`
 color: var(--n-item-text-color-child-active-horizontal);
 `),y("extra",`
 color: var(--n-item-text-color-child-active-horizontal);
 `)]),y("icon",`
 color: var(--n-item-icon-color-child-active-horizontal);
 `)]),ne("disabled",[ne("selected, child-active",[w("&:focus-within",Ke)]),E("selected",[Z(null,[y("icon","color: var(--n-item-icon-color-active-hover-horizontal);"),z("menu-item-content-header",`
 color: var(--n-item-text-color-active-hover-horizontal);
 `,[w("a","color: var(--n-item-text-color-active-hover-horizontal);"),y("extra","color: var(--n-item-text-color-active-hover-horizontal);")])])]),E("child-active",[Z(null,[y("icon","color: var(--n-item-icon-color-child-active-hover-horizontal);"),z("menu-item-content-header",`
 color: var(--n-item-text-color-child-active-hover-horizontal);
 `,[w("a","color: var(--n-item-text-color-child-active-hover-horizontal);"),y("extra","color: var(--n-item-text-color-child-active-hover-horizontal);")])])]),Z("border-bottom: 2px solid var(--n-border-color-horizontal);",Ke)]),z("menu-item-content-header",[w("a","color: var(--n-item-text-color-horizontal);")])])]),ne("responsive",[z("menu-item-content-header",`
 overflow: hidden;
 text-overflow: ellipsis;
 `)]),E("collapsed",[z("menu-item-content",[E("selected",[w("&::before",`
 background-color: var(--n-item-color-active-collapsed) !important;
 `)]),z("menu-item-content-header","opacity: 0;"),y("arrow","opacity: 0;"),y("icon","color: var(--n-item-icon-color-collapsed);")])]),z("menu-item",`
 height: var(--n-item-height);
 margin-top: 6px;
 position: relative;
 `),z("menu-item-content",`
 box-sizing: border-box;
 line-height: 1.75;
 height: 100%;
 display: grid;
 grid-template-areas: "icon content arrow";
 grid-template-columns: auto 1fr auto;
 align-items: center;
 cursor: pointer;
 position: relative;
 padding-right: 18px;
 transition:
 background-color .3s var(--n-bezier),
 padding-left .3s var(--n-bezier),
 border-color .3s var(--n-bezier);
 `,[w("> *","z-index: 1;"),w("&::before",`
 z-index: auto;
 content: "";
 background-color: #0000;
 position: absolute;
 left: 8px;
 right: 8px;
 top: 0;
 bottom: 0;
 pointer-events: none;
 border-radius: var(--n-border-radius);
 transition: background-color .3s var(--n-bezier);
 `),E("disabled",`
 opacity: .45;
 cursor: not-allowed;
 `),E("collapsed",[y("arrow","transform: rotate(0);")]),E("selected",[w("&::before","background-color: var(--n-item-color-active);"),y("arrow","color: var(--n-arrow-color-active);"),y("icon","color: var(--n-item-icon-color-active);"),z("menu-item-content-header",`
 color: var(--n-item-text-color-active);
 `,[w("a","color: var(--n-item-text-color-active);"),y("extra","color: var(--n-item-text-color-active);")])]),E("child-active",[z("menu-item-content-header",`
 color: var(--n-item-text-color-child-active);
 `,[w("a",`
 color: var(--n-item-text-color-child-active);
 `),y("extra",`
 color: var(--n-item-text-color-child-active);
 `)]),y("arrow",`
 color: var(--n-arrow-color-child-active);
 `),y("icon",`
 color: var(--n-item-icon-color-child-active);
 `)]),ne("disabled",[ne("selected, child-active",[w("&:focus-within",Te)]),E("selected",[Z(null,[y("arrow","color: var(--n-arrow-color-active-hover);"),y("icon","color: var(--n-item-icon-color-active-hover);"),z("menu-item-content-header",`
 color: var(--n-item-text-color-active-hover);
 `,[w("a","color: var(--n-item-text-color-active-hover);"),y("extra","color: var(--n-item-text-color-active-hover);")])])]),E("child-active",[Z(null,[y("arrow","color: var(--n-arrow-color-child-active-hover);"),y("icon","color: var(--n-item-icon-color-child-active-hover);"),z("menu-item-content-header",`
 color: var(--n-item-text-color-child-active-hover);
 `,[w("a","color: var(--n-item-text-color-child-active-hover);"),y("extra","color: var(--n-item-text-color-child-active-hover);")])])]),E("selected",[Z(null,[w("&::before","background-color: var(--n-item-color-active-hover);")])]),Z(null,Te)]),y("icon",`
 grid-area: icon;
 color: var(--n-item-icon-color);
 transition:
 color .3s var(--n-bezier),
 font-size .3s var(--n-bezier),
 margin-right .3s var(--n-bezier);
 box-sizing: content-box;
 display: inline-flex;
 align-items: center;
 justify-content: center;
 `),y("arrow",`
 grid-area: arrow;
 font-size: 16px;
 color: var(--n-arrow-color);
 transform: rotate(180deg);
 opacity: 1;
 transition:
 color .3s var(--n-bezier),
 transform 0.2s var(--n-bezier),
 opacity 0.2s var(--n-bezier);
 `),z("menu-item-content-header",`
 grid-area: content;
 transition:
 color .3s var(--n-bezier),
 opacity .3s var(--n-bezier);
 opacity: 1;
 white-space: nowrap;
 color: var(--n-item-text-color);
 `,[w("a",`
 outline: none;
 text-decoration: none;
 transition: color .3s var(--n-bezier);
 color: var(--n-item-text-color);
 `,[w("&::before",`
 content: "";
 position: absolute;
 left: 0;
 right: 0;
 top: 0;
 bottom: 0;
 `)]),y("extra",`
 font-size: .93em;
 color: var(--n-group-text-color);
 transition: color .3s var(--n-bezier);
 `)])]),z("submenu",`
 cursor: pointer;
 position: relative;
 margin-top: 6px;
 `,[z("menu-item-content",`
 height: var(--n-item-height);
 `),z("submenu-children",`
 overflow: hidden;
 padding: 0;
 `,[xo({duration:".2s"})])]),z("menu-item-group",[z("menu-item-group-title",`
 margin-top: 6px;
 color: var(--n-group-text-color);
 cursor: default;
 font-size: .93em;
 height: 36px;
 display: flex;
 align-items: center;
 transition:
 padding-left .3s var(--n-bezier),
 color .3s var(--n-bezier);
 `)])]),z("menu-tooltip",[w("a",`
 color: inherit;
 text-decoration: none;
 `)]),z("menu-divider",`
 transition: background-color .3s var(--n-bezier);
 background-color: var(--n-divider-color);
 height: 1px;
 margin: 6px 18px;
 `)]);function Z(e,n){return[E("hover",e,n),w("&:hover",e,n)]}var Do=j({name:"MenuDivider",setup(){const{mergedClsPrefixRef:e,isHorizontalRef:n}=G(se);return()=>n.value?null:(v(),S("div",{key:1,class:B(`${e.value}-menu-divider`)},null,2))}}),Uo=j({name:"ChevronDownFilled",render(){return(()=>{const e=_e("f3af82a2aab086a5");return e[0]||(e[0]=h("svg",{viewBox:"0 0 16 16",fill:"none",xmlns:"http://www.w3.org/2000/svg"},[h("path",{d:"M3.20041 5.73966C3.48226 5.43613 3.95681 5.41856 4.26034 5.70041L8 9.22652L11.7397 5.70041C12.0432 5.41856 12.5177 5.43613 12.7996 5.73966C13.0815 6.0432 13.0639 6.51775 12.7603 6.7996L8.51034 10.7996C8.22258 11.0668 7.77743 11.0668 7.48967 10.7996L3.23966 6.7996C2.93613 6.51775 2.91856 6.0432 3.20041 5.73966Z",fill:"currentColor"})],-1))})()}});const Go=["onClick"];var Xe=j({name:"MenuOptionContent",props:{collapsed:Boolean,disabled:Boolean,title:[String,Function],icon:Function,extra:[String,Function],showArrow:Boolean,childActive:Boolean,hover:Boolean,paddingLeft:Number,selected:Boolean,maxIconSize:{type:Number,required:!0},activeIconSize:{type:Number,required:!0},iconMarginRight:{type:Number,required:!0},clsPrefix:{type:String,required:!0},onClick:Function,tmNode:{type:Object,required:!0},isEllipsisPlaceholder:Boolean},setup(e){const{props:n}=G(se);return{menuProps:n,style:C(()=>{const{paddingLeft:r}=e;return{paddingLeft:r&&`${r}px`}}),iconStyle:C(()=>{const{maxIconSize:r,activeIconSize:l,iconMarginRight:s}=e;return{width:`${r}px`,height:`${r}px`,fontSize:`${l}px`,marginRight:`${s}px`}})}},render(){const{clsPrefix:e,tmNode:n,menuProps:{renderIcon:r,renderLabel:l,renderExtra:s,expandIcon:a}}=this,d=r?r(n.rawNode):oe(this.icon);return(()=>{const u=_e("7bb10afc6caf8fa4");return v(),S("div",{onClick:c=>{this.onClick?.(c)},role:"none",class:B([`${e}-menu-item-content`,{[`${e}-menu-item-content--selected`]:this.selected,[`${e}-menu-item-content--collapsed`]:this.collapsed,[`${e}-menu-item-content--child-active`]:this.childActive,[`${e}-menu-item-content--disabled`]:this.disabled,[`${e}-menu-item-content--hover`]:this.hover}]),style:me(this.style)},[_(()=>d&&(v(),S("div",{class:B(`${e}-menu-item-content__icon`),style:me(this.iconStyle),role:"none"},[_(()=>[d])],6))),h("div",{class:B(`${e}-menu-item-content-header`),role:"none"},[this.isEllipsisPlaceholder?(v(),S(U,{key:0},[_(()=>this.title)],64)):(v(),S(U,{key:1},[l?(v(),S(U,{key:0},[_(()=>l(n.rawNode))],64)):(v(),S(U,{key:1},[_(()=>oe(this.title))],64))],64)),this.extra||s?(v(),S("span",{key:2,class:B(`${e}-menu-item-content-header__extra`)},[u[0]||(u[0]=_(" ",-1)),s?(v(),S(U,{key:0},[_(()=>s(n.rawNode))],64)):(v(),S(U,{key:1},[_(()=>oe(this.extra))],64))],2)):_(()=>null)],2),this.showArrow?(v(),O(Co,{key:0,ariaHidden:!0,class:B(`${e}-menu-item-content__arrow`),clsPrefix:e},{default:()=>a?a(n.rawNode):(v(),O(Uo,{key:1}))},1032,["class","clsPrefix"])):_(()=>null)],14,Go)})()}});const ve=8;function He(e){const n=G(se),{props:r,mergedCollapsedRef:l}=n,s=G(Ze,null),a=G(Ne,null),d=C(()=>r.mode==="horizontal"),u=C(()=>d.value?r.dropdownPlacement:"tmNodes"in e?"right-start":"right"),c=C(()=>Math.max(r.collapsedIconSize??r.iconSize,r.iconSize));return{dropdownPlacement:u,activeIconSize:C(()=>!d.value&&e.root&&l.value?r.collapsedIconSize??r.iconSize:r.iconSize),maxIconSize:c,paddingLeft:C(()=>{if(d.value)return;const{collapsedWidth:g,indent:p,rootIndent:i}=r,{root:m,isGroup:N}=e,F=i===void 0?p:i;return m?l.value?g/2-c.value/2:F:a&&typeof a.paddingLeftRef.value=="number"?l.value?g/2-c.value/2:p/2+a.paddingLeftRef.value:s&&typeof s.paddingLeftRef.value=="number"?(N?p/2:p)+s.paddingLeftRef.value:0}),iconMarginRight:C(()=>{const{collapsedWidth:g,indent:p,rootIndent:i}=r,{value:m}=c,{root:N}=e;return d.value||!N||!l.value?ve:(i===void 0?p:i)+m+ve-(g+m)/2}),NMenu:n,NSubmenu:s,NMenuOptionGroup:a}}const Me={internalKey:{type:[String,Number],required:!0},root:Boolean,isGroup:Boolean,level:{type:Number,required:!0},title:[String,Function],extra:[String,Function]},Ye={...Me,tmNode:{type:Object,required:!0},disabled:Boolean,icon:Function,onClick:Function},qo=Pe(Ye),Wo=j({name:"MenuOption",props:Ye,setup(e){const n=He(e),{NSubmenu:r,NMenu:l,NMenuOptionGroup:s}=n,{props:a,mergedClsPrefixRef:d,mergedCollapsedRef:u}=l,c=r?r.mergedDisabledRef:s?s.mergedDisabledRef:{value:!1},g=C(()=>c.value||e.disabled);function p(m){const{onClick:N}=e;N&&N(m)}function i(m){g.value||(l.doSelect(e.internalKey,e.tmNode.rawNode),p(m))}return{mergedClsPrefix:d,dropdownPlacement:n.dropdownPlacement,paddingLeft:n.paddingLeft,iconMarginRight:n.iconMarginRight,maxIconSize:n.maxIconSize,activeIconSize:n.activeIconSize,mergedTheme:l.mergedThemeRef,menuProps:a,dropdownEnabled:Ie(()=>e.root&&u.value&&a.mode!=="horizontal"&&!g.value),selected:Ie(()=>l.mergedValueRef.value===e.internalKey),mergedDisabled:g,handleClick:i}},render(){const{mergedClsPrefix:e,mergedTheme:n,tmNode:r,menuProps:{renderLabel:l,nodeProps:s}}=this,a=s?.(r.rawNode);return v(),S("div",te(a,{role:"menuitem",class:[`${e}-menu-item`,a?.class]}),[(v(),O(ee,{theme:n.peers.Tooltip,themeOverrides:n.peerOverrides.Tooltip,trigger:"hover",placement:this.dropdownPlacement,disabled:!this.dropdownEnabled||this.title===void 0,internalExtraClass:["menu-tooltip"]},{default:()=>l?l(r.rawNode):oe(this.title),trigger:()=>(v(),O(Xe,{tmNode:r,clsPrefix:e,paddingLeft:this.paddingLeft,iconMarginRight:this.iconMarginRight,maxIconSize:this.maxIconSize,activeIconSize:this.activeIconSize,selected:this.selected,title:this.title,extra:this.extra,disabled:this.mergedDisabled,icon:this.icon,onClick:this.handleClick},null,8,["tmNode","clsPrefix","paddingLeft","iconMarginRight","maxIconSize","activeIconSize","selected","title","extra","disabled","icon","onClick"]))},1032,["theme","themeOverrides","placement","disabled"]))],16)}}),Je={...Me,tmNode:{type:Object,required:!0},tmNodes:{type:Array,required:!0}},Zo=Pe(Je),Xo=j({name:"MenuOptionGroup",props:Je,setup(e){const n=He(e),{NSubmenu:r}=n,l=C(()=>r?.mergedDisabledRef.value?!0:e.tmNode.disabled);he(Ne,{paddingLeftRef:n.paddingLeft,mergedDisabledRef:l});const{mergedClsPrefixRef:s,props:a}=G(se);return function(){const{value:d}=s,u=n.paddingLeft.value,{nodeProps:c}=a,g=c?.(e.tmNode.rawNode);return(()=>{const p=_e("45eca6a63be5028b");return v(),S("div",{class:B(`${d}-menu-item-group`),role:"group"},[h("div",te(g,{class:[`${d}-menu-item-group-title`,g?.class],style:[g?.style||"",u!==void 0?`padding-left: ${u}px;`:""]}),[_(()=>oe(e.title)),e.extra?(v(),S(U,{key:0},[p[0]||(p[0]=_(" ",-1)),_(()=>oe(e.extra))],64)):_(()=>null)],16),h("div",null,[_(()=>e.tmNodes.map(i=>Ee(i,a)))])],2)})()}}}),Yo=["aria-expanded","id"],Jo=["aria-expanded","id"],Qe={...Me,rawNodes:{type:Array,default:()=>[]},tmNodes:{type:Array,default:()=>[]},tmNode:{type:Object,required:!0},disabled:Boolean,icon:Function,onClick:Function,domId:String,virtualChildActive:{type:Boolean,default:void 0},isEllipsisPlaceholder:Boolean},Qo=Pe(Qe),Re=j({name:"Submenu",props:Qe,setup(e){const n=He(e),{NMenu:r,NSubmenu:l}=n,{props:s,mergedCollapsedRef:a,mergedThemeRef:d}=r,u=C(()=>{const{disabled:m}=e;return l?.mergedDisabledRef.value||s.disabled?!0:m}),c=V(!1);he(Ze,{paddingLeftRef:n.paddingLeft,mergedDisabledRef:u}),he(Ne,null);function g(){const{onClick:m}=e;m&&m()}function p(){u.value||(a.value||r.toggleExpand(e.internalKey),g())}function i(m){c.value=m}return{menuProps:s,mergedTheme:d,doSelect:r.doSelect,inverted:r.invertedRef,isHorizontal:r.isHorizontalRef,mergedClsPrefix:r.mergedClsPrefixRef,maxIconSize:n.maxIconSize,activeIconSize:n.activeIconSize,iconMarginRight:n.iconMarginRight,dropdownPlacement:n.dropdownPlacement,dropdownShow:c,paddingLeft:n.paddingLeft,mergedDisabled:u,mergedValue:r.mergedValueRef,childActive:Ie(()=>e.virtualChildActive??r.activePathRef.value.includes(e.internalKey)),collapsed:C(()=>s.mode==="horizontal"?!1:a.value?!0:!r.mergedExpandedKeysRef.value.includes(e.internalKey)),dropdownEnabled:C(()=>!u.value&&(s.mode==="horizontal"||a.value)),handlePopoverShowChange:i,handleClick:p}},render(){const{mergedClsPrefix:e,menuProps:{renderIcon:n,renderLabel:r}}=this,l=()=>{const{isHorizontal:a,paddingLeft:d,collapsed:u,mergedDisabled:c,maxIconSize:g,activeIconSize:p,title:i,childActive:m,icon:N,handleClick:F,menuProps:{nodeProps:$},dropdownShow:H,iconMarginRight:D,tmNode:b,mergedClsPrefix:A,isEllipsisPlaceholder:L,extra:K}=this,M=$?.(b.rawNode);return v(),S("div",te(M,{class:[`${A}-menu-item`,M?.class],role:"menuitem"}),[(v(),O(Xe,{tmNode:b,paddingLeft:d,collapsed:u,disabled:c,iconMarginRight:D,maxIconSize:g,activeIconSize:p,title:i,extra:K,showArrow:!a,childActive:m,clsPrefix:A,icon:N,hover:H,onClick:F,isEllipsisPlaceholder:L},null,8,["tmNode","paddingLeft","collapsed","disabled","iconMarginRight","maxIconSize","activeIconSize","title","extra","showArrow","childActive","clsPrefix","icon","hover","onClick","isEllipsisPlaceholder"]))],16)},s=()=>(v(),O(yo,null,{default:()=>{const{tmNodes:a,collapsed:d}=this;return d?null:(v(),S("div",{key:1,class:B(`${e}-submenu-children`),role:"menu"},[_(()=>a.map(u=>Ee(u,this.menuProps)))],2))}},1024));return this.root?(v(),O(qe,te({key:2,size:"large",trigger:"hover"},this.menuProps?.dropdownProps,{themeOverrides:this.mergedTheme.peerOverrides.Dropdown,theme:this.mergedTheme.peers.Dropdown,builtinThemeOverrides:{fontSizeLarge:"14px",optionIconSizeLarge:"18px"},value:this.mergedValue,disabled:!this.dropdownEnabled,placement:this.dropdownPlacement,keyField:this.menuProps.keyField,labelField:this.menuProps.labelField,childrenField:this.menuProps.childrenField,onUpdateShow:this.handlePopoverShowChange,options:this.rawNodes,onSelect:this.doSelect,inverted:this.inverted,renderIcon:n,renderLabel:r}),{default:()=>(v(),S("div",{class:B(`${e}-submenu`),role:"menu","aria-expanded":!this.collapsed,id:this.domId},[_(()=>l()),this.isHorizontal?_(()=>null):(v(),S(U,{key:1},[_(()=>s())],64))],10,Yo))},1040,["themeOverrides","theme","value","disabled","placement","keyField","labelField","childrenField","onUpdateShow","options","onSelect","inverted","renderIcon","renderLabel"])):(v(),S("div",{key:3,class:B(`${e}-submenu`),role:"menu","aria-expanded":!this.collapsed,id:this.domId},[_(()=>l()),_(()=>s())],10,Jo))}});function ke(e){return e.type==="divider"||e.type==="render"}function et(e){return e.type==="divider"}function Ee(e,n){const{rawNode:r}=e,{show:l}=r;if(l===!1)return null;if(ke(r))return et(r)?(v(),O(Do,te({key:e.key},r.props),null,16)):null;const{labelField:s}=n,{key:a,level:d,isGroup:u}=e,c={...r,title:r.title||r[s],extra:r.titleExtra||r.extra,key:a,internalKey:a,level:d,root:d===0,isGroup:u};return e.children?e.isGroup?ae(Xo,ge(c,Zo,{tmNode:e,tmNodes:e.children,key:a})):ae(Re,ge(c,Qo,{key:a,rawNodes:r[n.childrenField],tmNodes:e.children,tmNode:e})):ae(Wo,ge(c,qo,{key:a,tmNode:e}))}const ot={...fe.props,options:{type:Array,default:()=>[]},collapsed:{type:Boolean,default:void 0},collapsedWidth:{type:Number,default:48},iconSize:{type:Number,default:20},collapsedIconSize:{type:Number,default:24},rootIndent:Number,indent:{type:Number,default:32},labelField:{type:String,default:"label"},keyField:{type:String,default:"key"},childrenField:{type:String,default:"children"},disabledField:{type:String,default:"disabled"},defaultExpandAll:Boolean,defaultExpandedKeys:Array,expandedKeys:Array,value:[String,Number],defaultValue:{type:[String,Number],default:null},mode:{type:String,default:"vertical"},watchProps:{type:Array,default:void 0},disabled:Boolean,show:{type:Boolean,default:!0},inverted:Boolean,"onUpdate:expandedKeys":[Function,Array],onUpdateExpandedKeys:[Function,Array],onUpdateValue:[Function,Array],"onUpdate:value":[Function,Array],expandIcon:Function,renderIcon:Function,renderLabel:Function,renderExtra:Function,dropdownProps:Object,accordion:Boolean,nodeProps:Function,dropdownPlacement:{type:String,default:"bottom"},responsive:Boolean,items:Array,onOpenNamesChange:[Function,Array],onSelect:[Function,Array],onExpandedNamesChange:[Function,Array],expandedNames:Array,defaultExpandedNames:Array};var tt=j({name:"Menu",inheritAttrs:!1,props:ot,setup(e){const{mergedClsPrefixRef:n,inlineThemeDisabled:r}=Be(e),l=fe("Menu","-menu",jo,wo,e,n),s=G(Vo,null),a=C(()=>{const{collapsed:f}=e;if(f!==void 0)return f;if(s){const{collapseModeRef:I,collapsedRef:o}=s;if(I.value==="width")return o.value??!1}return!1}),d=C(()=>{const{keyField:f,childrenField:I,disabledField:o}=e;return xe(e.items||e.options,{getIgnored(x){return ke(x)},getChildren(x){return x[I]},getDisabled(x){return x[o]},getKey(x){return x[f]??x.name}})}),u=C(()=>new Set(d.value.treeNodes.map(f=>f.key))),{watchProps:c}=e,g=V(null);c?.includes("defaultValue")?we(()=>{g.value=e.defaultValue}):g.value=e.defaultValue;const p=Oe(e,"value"),i=Le(p,g),m=V([]),N=()=>{m.value=e.defaultExpandAll?d.value.getNonLeafKeys():e.defaultExpandedNames||e.defaultExpandedKeys||d.value.getPath(i.value,{includeSelf:!1}).keyPath};c?.includes("defaultExpandedKeys")?we(N):N();const F=Mo(e,["expandedNames","expandedKeys"]),$=Le(F,m),H=C(()=>d.value.treeNodes),D=C(()=>d.value.getPath(i.value).keyPath);he(se,{props:e,mergedCollapsedRef:a,mergedThemeRef:l,mergedValueRef:i,mergedExpandedKeysRef:$,activePathRef:D,mergedClsPrefixRef:n,isHorizontalRef:C(()=>e.mode==="horizontal"),invertedRef:Oe(e,"inverted"),doSelect:b,toggleExpand:L});function b(f,I){const{"onUpdate:value":o,onUpdateValue:x,onSelect:T}=e;x&&W(x,f,I),o&&W(o,f,I),T&&W(T,f,I),g.value=f}function A(f){const{"onUpdate:expandedKeys":I,onUpdateExpandedKeys:o,onExpandedNamesChange:x,onOpenNamesChange:T}=e;I&&W(I,f),o&&W(o,f),x&&W(x,f),T&&W(T,f),m.value=f}function L(f){const I=Array.from($.value),o=I.findIndex(x=>x===f);if(~o)I.splice(o,1);else{if(e.accordion&&u.value.has(f)){const x=I.findIndex(T=>u.value.has(T));x>-1&&I.splice(x,1)}I.push(f)}A(I)}const K=f=>{const I=d.value.getPath(f??i.value,{includeSelf:!1}).keyPath;if(!I.length)return;const o=Array.from($.value),x=new Set([...o,...I]);e.accordion&&u.value.forEach(T=>{x.has(T)&&!I.includes(T)&&x.delete(T)}),A(Array.from(x))},M=C(()=>{const{inverted:f}=e,{common:{cubicBezierEaseInOut:I},self:o}=l.value,{borderRadius:x,borderColorHorizontal:T,fontSize:ao,itemHeight:lo,dividerColor:so}=o,t={"--n-divider-color":so,"--n-bezier":I,"--n-font-size":ao,"--n-border-color-horizontal":T,"--n-border-radius":x,"--n-item-height":lo};return f?(t["--n-group-text-color"]=o.groupTextColorInverted,t["--n-color"]=o.colorInverted,t["--n-item-text-color"]=o.itemTextColorInverted,t["--n-item-text-color-hover"]=o.itemTextColorHoverInverted,t["--n-item-text-color-active"]=o.itemTextColorActiveInverted,t["--n-item-text-color-child-active"]=o.itemTextColorChildActiveInverted,t["--n-item-text-color-child-active-hover"]=o.itemTextColorChildActiveInverted,t["--n-item-text-color-active-hover"]=o.itemTextColorActiveHoverInverted,t["--n-item-icon-color"]=o.itemIconColorInverted,t["--n-item-icon-color-hover"]=o.itemIconColorHoverInverted,t["--n-item-icon-color-active"]=o.itemIconColorActiveInverted,t["--n-item-icon-color-active-hover"]=o.itemIconColorActiveHoverInverted,t["--n-item-icon-color-child-active"]=o.itemIconColorChildActiveInverted,t["--n-item-icon-color-child-active-hover"]=o.itemIconColorChildActiveHoverInverted,t["--n-item-icon-color-collapsed"]=o.itemIconColorCollapsedInverted,t["--n-item-text-color-horizontal"]=o.itemTextColorHorizontalInverted,t["--n-item-text-color-hover-horizontal"]=o.itemTextColorHoverHorizontalInverted,t["--n-item-text-color-active-horizontal"]=o.itemTextColorActiveHorizontalInverted,t["--n-item-text-color-child-active-horizontal"]=o.itemTextColorChildActiveHorizontalInverted,t["--n-item-text-color-child-active-hover-horizontal"]=o.itemTextColorChildActiveHoverHorizontalInverted,t["--n-item-text-color-active-hover-horizontal"]=o.itemTextColorActiveHoverHorizontalInverted,t["--n-item-icon-color-horizontal"]=o.itemIconColorHorizontalInverted,t["--n-item-icon-color-hover-horizontal"]=o.itemIconColorHoverHorizontalInverted,t["--n-item-icon-color-active-horizontal"]=o.itemIconColorActiveHorizontalInverted,t["--n-item-icon-color-active-hover-horizontal"]=o.itemIconColorActiveHoverHorizontalInverted,t["--n-item-icon-color-child-active-horizontal"]=o.itemIconColorChildActiveHorizontalInverted,t["--n-item-icon-color-child-active-hover-horizontal"]=o.itemIconColorChildActiveHoverHorizontalInverted,t["--n-arrow-color"]=o.arrowColorInverted,t["--n-arrow-color-hover"]=o.arrowColorHoverInverted,t["--n-arrow-color-active"]=o.arrowColorActiveInverted,t["--n-arrow-color-active-hover"]=o.arrowColorActiveHoverInverted,t["--n-arrow-color-child-active"]=o.arrowColorChildActiveInverted,t["--n-arrow-color-child-active-hover"]=o.arrowColorChildActiveHoverInverted,t["--n-item-color-hover"]=o.itemColorHoverInverted,t["--n-item-color-active"]=o.itemColorActiveInverted,t["--n-item-color-active-hover"]=o.itemColorActiveHoverInverted,t["--n-item-color-active-collapsed"]=o.itemColorActiveCollapsedInverted):(t["--n-group-text-color"]=o.groupTextColor,t["--n-color"]=o.color,t["--n-item-text-color"]=o.itemTextColor,t["--n-item-text-color-hover"]=o.itemTextColorHover,t["--n-item-text-color-active"]=o.itemTextColorActive,t["--n-item-text-color-child-active"]=o.itemTextColorChildActive,t["--n-item-text-color-child-active-hover"]=o.itemTextColorChildActiveHover,t["--n-item-text-color-active-hover"]=o.itemTextColorActiveHover,t["--n-item-icon-color"]=o.itemIconColor,t["--n-item-icon-color-hover"]=o.itemIconColorHover,t["--n-item-icon-color-active"]=o.itemIconColorActive,t["--n-item-icon-color-active-hover"]=o.itemIconColorActiveHover,t["--n-item-icon-color-child-active"]=o.itemIconColorChildActive,t["--n-item-icon-color-child-active-hover"]=o.itemIconColorChildActiveHover,t["--n-item-icon-color-collapsed"]=o.itemIconColorCollapsed,t["--n-item-text-color-horizontal"]=o.itemTextColorHorizontal,t["--n-item-text-color-hover-horizontal"]=o.itemTextColorHoverHorizontal,t["--n-item-text-color-active-horizontal"]=o.itemTextColorActiveHorizontal,t["--n-item-text-color-child-active-horizontal"]=o.itemTextColorChildActiveHorizontal,t["--n-item-text-color-child-active-hover-horizontal"]=o.itemTextColorChildActiveHoverHorizontal,t["--n-item-text-color-active-hover-horizontal"]=o.itemTextColorActiveHoverHorizontal,t["--n-item-icon-color-horizontal"]=o.itemIconColorHorizontal,t["--n-item-icon-color-hover-horizontal"]=o.itemIconColorHoverHorizontal,t["--n-item-icon-color-active-horizontal"]=o.itemIconColorActiveHorizontal,t["--n-item-icon-color-active-hover-horizontal"]=o.itemIconColorActiveHoverHorizontal,t["--n-item-icon-color-child-active-horizontal"]=o.itemIconColorChildActiveHorizontal,t["--n-item-icon-color-child-active-hover-horizontal"]=o.itemIconColorChildActiveHoverHorizontal,t["--n-arrow-color"]=o.arrowColor,t["--n-arrow-color-hover"]=o.arrowColorHover,t["--n-arrow-color-active"]=o.arrowColorActive,t["--n-arrow-color-active-hover"]=o.arrowColorActiveHover,t["--n-arrow-color-child-active"]=o.arrowColorChildActive,t["--n-arrow-color-child-active-hover"]=o.arrowColorChildActiveHover,t["--n-item-color-hover"]=o.itemColorHover,t["--n-item-color-active"]=o.itemColorActive,t["--n-item-color-active-hover"]=o.itemColorActiveHover,t["--n-item-color-active-collapsed"]=o.itemColorActiveCollapsed),t}),Y=r?Ve("menu",C(()=>e.inverted?"a":"b"),M,e):void 0,q=zo(),J=V(null),pe=V(null);let ce=!0;const de=()=>{ce?ce=!1:J.value?.sync({showAllItemsBeforeCalculate:!0})};function re(){return document.getElementById(q)}const ue=V(-1);function eo(f){ue.value=e.options.length-f}function oo(f){f||(ue.value=-1)}const to=C(()=>{const f=ue.value;return{children:f===-1?[]:e.options.slice(f)}}),ro=C(()=>{const{childrenField:f,disabledField:I,keyField:o}=e;return xe([to.value],{getIgnored(x){return ke(x)},getChildren(x){return x[f]},getDisabled(x){return x[I]},getKey(x){return x[o]??x.name}})}),no=C(()=>xe([{}]).treeNodes[0]);function io(){if(ue.value===-1)return v(),O(Re,{root:!0,level:0,key:"__ellpisisGroupPlaceholder__",internalKey:"__ellpisisGroupPlaceholder__",title:"···",tmNode:no.value,domId:q,isEllipsisPlaceholder:!0},null,8,["tmNode","domId"]);const f=ro.value.treeNodes[0],I=D.value,o=!!f.children?.some(x=>I.includes(x.key));return v(),O(Re,{level:0,root:!0,key:"__ellpisisGroup__",internalKey:"__ellpisisGroup__",title:"···",virtualChildActive:o,tmNode:f,domId:q,rawNodes:f.rawNode.children||[],tmNodes:f.children||[],isEllipsisPlaceholder:!0},null,8,["virtualChildActive","tmNode","domId","rawNodes","tmNodes"])}return{mergedClsPrefix:n,controlledExpandedKeys:F,uncontrolledExpanededKeys:m,mergedExpandedKeys:$,uncontrolledValue:g,mergedValue:i,activePath:D,tmNodes:H,mergedTheme:l,mergedCollapsed:a,cssVars:r?void 0:M,themeClass:Y?.themeClass,overflowRef:J,counterRef:pe,updateCounter:()=>{},onResize:de,onUpdateOverflow:oo,onUpdateCount:eo,renderCounter:io,getCounter:re,onRender:Y?.onRender,showOption:K,deriveResponsiveState:de}},render(){const{mergedClsPrefix:e,mode:n,themeClass:r,onRender:l}=this;l?.();const s=()=>this.tmNodes.map(u=>Ee(u,this.$props)),a=n==="horizontal"&&this.responsive,d=()=>ae("div",te(this.$attrs,{role:n==="horizontal"?"menubar":"menu",class:[`${e}-menu`,r,`${e}-menu--${n}`,a&&`${e}-menu--responsive`,this.mergedCollapsed&&`${e}-menu--collapsed`],style:this.cssVars}),a?(v(),O(No,{key:2,ref:"overflowRef",onUpdateOverflow:this.onUpdateOverflow,getCounter:this.getCounter,onUpdateCount:this.onUpdateCount,updateCounter:this.updateCounter,style:{width:"100%",display:"flex",overflow:"hidden"}},{default:s,counter:this.renderCounter},1032,["onUpdateOverflow","getCounter","onUpdateCount","updateCounter"])):s());return a?(v(),O(De,{key:3,onResize:this.onResize},{default:d},1032,["onResize"])):d()}});const rt={class:"rail","aria-label":"Servers"},nt={class:"rail-nav",role:"list","aria-label":"Managed servers"},it={class:"rail-item"},at={class:"rail-foot"},lt={class:"rail-item"},st={class:"rail-item"},ct={key:0,class:"rail-badge","aria-hidden":"true"},dt=j({__name:"ServerRail",setup(e){const n=Ho(),r=Ue(),l=C(()=>r.name==="dashboard"),s=C(()=>n.servers.filter(p=>p.status==="offline"||p.status==="error").length),a=C(()=>s.value===0?"No new alerts":`${s.value} new alert${s.value===1?"":"s"}`),d={ready:"dot--online",validating:"dot--idle",pending:"dot--idle",offline:"dot--offline",error:"dot--dnd"},u={ready:"Ready",validating:"Validating",pending:"Pending",offline:"Offline",error:"Error"};function c(p){const i=p.split(/[^A-Za-z0-9]+/).filter(m=>m.length>0);return i.length===0?"?":i.length===1?i[0].slice(0,2).toUpperCase():`${i[0][0]}${i[1][0]}`.toUpperCase()}function g(p){return`${p.name} · ${u[p.status]??p.status}`}return je(()=>{n.servers.length===0&&n.fetchServers().catch(()=>{}),n.pollServers()}),Ae(()=>r.path,()=>{n.pollServers()}),Io(()=>{n.stopPolling()}),(p,i)=>(v(),S("nav",rt,[h("div",{class:Se(["rail-item",{"is-active":l.value}])},[P(R(ee),{placement:"right",trigger:"hover"},{trigger:k(()=>[P(R(Q),{class:"rail-btn",to:{name:"dashboard"},"aria-label":"Gotham — home"},{default:k(()=>[...i[0]||(i[0]=[h("svg",{viewBox:"0 0 24 24",fill:"none",stroke:"currentColor","stroke-width":"1.8","stroke-linecap":"round","aria-hidden":"true"},[h("path",{d:"M5 19V11a7 7 0 0114 0v8"}),h("path",{d:"M9.5 19v-6.5a2.5 2.5 0 015 0V19"})],-1)])]),_:1})]),default:k(()=>[i[1]||(i[1]=h("span",null,"Gotham — home",-1))]),_:1})],2),i[8]||(i[8]=h("div",{class:"rail-sep","aria-hidden":"true"},null,-1)),h("div",nt,[(v(!0),S(U,null,So(R(n).servers,m=>(v(),S("div",{key:m.id,class:"rail-item",role:"listitem"},[P(R(ee),{placement:"right",trigger:"hover"},{trigger:k(()=>[P(R(Q),{class:"rail-btn",to:{name:"servers"},"aria-label":g(m)},{default:k(()=>[ie(X(c(m.name))+" ",1),h("span",{class:Se(["rail-dot",d[m.status]]),"aria-hidden":"true"},null,2)]),_:2},1032,["aria-label"])]),default:k(()=>[h("span",null,X(g(m)),1)]),_:2},1024)]))),128))]),h("div",it,[P(R(ee),{placement:"right",trigger:"hover"},{trigger:k(()=>[P(R(Q),{class:"rail-btn rail-btn--add",to:{name:"servers"},"aria-label":"Add server"},{default:k(()=>[...i[2]||(i[2]=[h("svg",{viewBox:"0 0 24 24",fill:"none",stroke:"currentColor","stroke-width":"1.6","stroke-linecap":"round","stroke-linejoin":"round","aria-hidden":"true"},[h("path",{d:"M12 5v14M5 12h14"})],-1)])]),_:1})]),default:k(()=>[i[3]||(i[3]=h("span",null,"Add server",-1))]),_:1})]),h("div",at,[i[7]||(i[7]=h("div",{class:"rail-sep","aria-hidden":"true"},null,-1)),h("div",lt,[P(R(ee),{placement:"right",trigger:"hover"},{trigger:k(()=>[P(R(Q),{class:"rail-btn",to:{name:"dashboard"},"aria-label":"Overview"},{default:k(()=>[...i[4]||(i[4]=[h("svg",{viewBox:"0 0 24 24",fill:"none",stroke:"currentColor","stroke-width":"1.6","stroke-linecap":"round","stroke-linejoin":"round","aria-hidden":"true"},[h("path",{d:"M4 7h10M18 7h2M4 17h4M12 17h8"}),h("circle",{cx:"16",cy:"7",r:"2"}),h("circle",{cx:"10",cy:"17",r:"2"})],-1)])]),_:1})]),default:k(()=>[i[5]||(i[5]=h("span",null,"Overview",-1))]),_:1})]),h("div",st,[P(R(ee),{placement:"right",trigger:"hover"},{trigger:k(()=>[P(R(Q),{class:"rail-btn",to:{name:"servers"},"aria-label":`System alerts — ${a.value}`},{default:k(()=>[i[6]||(i[6]=h("svg",{viewBox:"0 0 24 24",fill:"none",stroke:"currentColor","stroke-width":"1.6","stroke-linecap":"round","stroke-linejoin":"round","aria-hidden":"true"},[h("path",{d:"M6 9a6 6 0 1112 0c0 5 2 6 2 6H4s2-1 2-6zM10 20a2 2 0 004 0"})],-1)),s.value>0?(v(),S("span",ct,X(s.value>9?"9+":s.value),1)):Ge("",!0)]),_:1},8,["aria-label"])]),default:k(()=>[h("span",null,X(a.value),1)]),_:1})])])]))}}),ut=We(dt,[["__scopeId","data-v-339e6bb5"]]),vt=Ro("app",{state:()=>({sidebarCollapsed:!1}),actions:{setSidebarCollapsed(e){this.sidebarCollapsed=e},toggleSidebar(){this.sidebarCollapsed=!this.sidebarCollapsed}}}),mt={class:"sidebar","aria-label":"Product navigation"},ht={class:"sidebar-body"},ft={class:"main"},pt={class:"topbar"},gt={class:"topbar-right"},bt={class:"view"},xt={class:"page"},Ct=j({__name:"AppLayout",setup(e){const n=vt(),r=ko(),l=Ue(),s=_o(),a=[{label:"Dashboard",key:"dashboard"},{label:"Servers",key:"servers"}],d=[{label:"Sign out",key:"sign-out"}],u=C(()=>String(l.name??"dashboard")),c=C(()=>l.meta.title??"Gotham"),g=C(()=>r.user?.email??""),p=C(()=>(r.user?.email?.[0]??"?").toUpperCase()),i=V(!1);function m($){s.push({name:String($)})}async function N($){$==="sign-out"&&(await r.logout(),await s.push({name:"login"}))}function F(){if(window.matchMedia("(max-width: 1024px)").matches){i.value=!i.value;return}n.toggleSidebar()}return Ae(()=>l.path,()=>{i.value=!1}),($,H)=>(v(),S("div",{class:Se(["app",{"is-collapsed":R(n).sidebarCollapsed,"is-open":i.value}])},[P(ut),h("aside",mt,[H[1]||(H[1]=h("div",{class:"sidebar-head"},"Gotham",-1)),h("div",ht,[P(R(tt),{value:u.value,options:a,"onUpdate:value":m},null,8,["value"])])]),h("div",ft,[h("header",pt,[P(R(be),{quaternary:"",circle:"",class:"nav-toggle","aria-label":"Toggle navigation",onClick:F},{icon:k(()=>[...H[2]||(H[2]=[h("svg",{viewBox:"0 0 24 24",fill:"none",stroke:"currentColor","stroke-width":"1.6","stroke-linecap":"round","stroke-linejoin":"round","aria-hidden":"true",width:"18",height:"18"},[h("path",{d:"M4 4h6v6H4zM14 4h6v6h-6zM4 14h6v6H4zM14 14h6v6h-6z"})],-1)])]),_:1}),P(R(Fe),{strong:""},{default:k(()=>[ie(X(c.value),1)]),_:1}),h("div",gt,[P(R($e),{align:"center"},{default:k(()=>[R(r).isAuthenticated?(v(),O(R(qe),{key:0,trigger:"click",options:d,onSelect:N},{default:k(()=>[P(R(be),{quaternary:""},{default:k(()=>[P(R($e),{align:"center",size:8},{default:k(()=>[P(R(Bo),{round:"",size:28,src:R(r).user?.avatar},{default:k(()=>[ie(X(p.value),1)]),_:1},8,["src"]),P(R(Fe),{depth:"2"},{default:k(()=>[ie(X(g.value),1)]),_:1})]),_:1})]),_:1})]),_:1})):(v(),O(R(Q),{key:1,to:"/login"},{default:k(()=>[P(R(be),{quaternary:"",type:"primary"},{default:k(()=>[...H[3]||(H[3]=[ie("Sign in",-1)])]),_:1})]),_:1}))]),_:1})])]),h("div",bt,[h("div",xt,[P(R(Ao))])])]),i.value?(v(),S("div",{key:0,class:"backdrop","aria-hidden":"true",onClick:H[0]||(H[0]=D=>i.value=!1)})):Ge("",!0)],2))}}),kt=We(Ct,[["__scopeId","data-v-ad21ffa8"]]);export{kt as default};
