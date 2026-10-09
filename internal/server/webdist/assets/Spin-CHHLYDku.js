<<<<<<<< HEAD:internal/server/webdist/assets/Spin-BUfoditl.js
<<<<<<<< HEAD:internal/server/webdist/assets/Spin-BUfoditl.js
import{$ as g,a0 as a,aT as $,as as b,d as w,f8 as T,a1 as k,o as d,c as u,a3 as p,al as i,a as z,ak as f,x as B,di as R,f as V,aG as N,a4 as P,au as W,aL as _,g as y,p as E,f9 as I,a7 as L,a6 as O}from"./index-De9orRk0.js";import{u as j}from"./use-compitable-tH8fn4DX.js";var D=g([g("@keyframes spin-rotate",`
========
import{$ as g,a0 as a,aT as $,as as b,d as w,fa as T,a1 as z,o as d,c as u,a3 as p,al as i,a as k,ak as f,x as B,dk as R,f as V,aG as N,a4 as P,au as W,aL as _,g as y,p as E,fb as I,a7 as L,a6 as O}from"./index-Cy-4-TeX.js";import{u as j}from"./use-compitable-DgfuJrNV.js";var D=g([g("@keyframes spin-rotate",`
>>>>>>>> feat/jus69-modal-scroll:internal/server/webdist/assets/Spin-BKH6w1Pw.js
========
import{$ as g,a0 as a,aT as $,as as b,d as w,fa as T,a1 as z,o as d,c as u,a3 as p,al as i,a as k,ak as f,x as B,dk as R,f as V,aG as N,a4 as P,au as W,aL as _,g as y,p as E,fb as I,a7 as L,a6 as O}from"./index-BTt6aOWT.js";import{u as j}from"./use-compitable-UrESpheG.js";var D=g([g("@keyframes spin-rotate",`
>>>>>>>> feat/jus74-connect-modals:internal/server/webdist/assets/Spin-CHHLYDku.js
 from {
 transform: rotate(0);
 }
 to {
 transform: rotate(360deg);
 }
 `),a("spin-container",`
 position: relative;
 `,[a("spin-body",`
 position: absolute;
 top: 50%;
 left: 50%;
 transform: translateX(-50%) translateY(-50%);
 `,[$()])]),a("spin-body",`
 display: inline-flex;
 align-items: center;
 justify-content: center;
 flex-direction: column;
 `),a("spin",`
 display: inline-flex;
 height: var(--n-size);
 width: var(--n-size);
 font-size: var(--n-size);
 color: var(--n-color);
 `,[b("rotate",`
 animation: spin-rotate 2s linear infinite;
 `)]),a("spin-description",`
 display: inline-block;
 font-size: var(--n-font-size);
 color: var(--n-text-color);
 transition: color .3s var(--n-bezier);
 margin-top: 8px;
 `),a("spin-content",`
 opacity: 1;
 transition: opacity .3s var(--n-bezier);
 pointer-events: all;
 `,[b("spinning",`
 user-select: none;
 -webkit-user-select: none;
 pointer-events: none;
 opacity: var(--n-opacity-spinning);
 `)])]);const H={small:20,medium:18,large:16},K={...k.props,contentClass:String,contentStyle:[Object,String],description:String,size:{type:[String,Number],default:"medium"},show:{type:Boolean,default:!0},rotate:{type:Boolean,default:!0},spinning:{type:Boolean,validator:()=>!0,default:void 0},delay:Number,...T,strokeWidth:Number};var X=w({name:"Spin",props:K,slots:Object,setup(e){const{mergedClsPrefixRef:t,inlineThemeDisabled:o}=P(e),m=k("Spin","-spin",D,I,e,t),r=y(()=>{const{size:s}=e,{common:{cubicBezierEaseInOut:n},self:c}=m.value,{opacitySpinning:S,color:x,textColor:C}=c;return{"--n-bezier":n,"--n-opacity-spinning":S,"--n-size":typeof s=="number"?L(s):c[O("size",s)],"--n-color":x,"--n-text-color":C}}),l=o?W("spin",y(()=>{const{size:s}=e;return typeof s=="number"?String(s):s[0]}),r,e):void 0,v=j(e,["spinning","show"]),h=E(!1);return _(s=>{let n;if(v.value){const{delay:c}=e;if(c){n=window.setTimeout(()=>{h.value=!0},c),s(()=>{clearTimeout(n)});return}}h.value=v.value}),{mergedClsPrefix:t,active:h,mergedStrokeWidth:y(()=>{const{strokeWidth:s}=e;if(s!==void 0)return s;const{size:n}=e;return H[typeof n=="number"?"medium":n]}),cssVars:o?void 0:r,themeClass:l?.themeClass,onRender:l?.onRender}},render(){const{$slots:e,mergedClsPrefix:t,description:o}=this,m=e.icon&&this.rotate,r=(o||e.description)&&(d(),u("div",{class:i(`${t}-spin-description`)},[p(()=>o||e.description?.())],2)),l=e.icon?(d(),u("div",{key:1,class:i([`${t}-spin-body`,this.themeClass])},[z("div",{class:i([`${t}-spin`,m&&`${t}-spin--rotate`]),style:f(e.default?"":this.cssVars)},[p(()=>e.icon())],6),p(()=>r)],2)):(d(),u("div",{key:2,class:i([`${t}-spin-body`,this.themeClass])},[(d(),B(R,{clsPrefix:t,style:f(e.default?"":this.cssVars),stroke:this.stroke,"stroke-width":this.mergedStrokeWidth,radius:this.radius,scale:this.scale,class:i(`${t}-spin`)},null,8,["clsPrefix","style","stroke","stroke-width","radius","scale","class"])),p(()=>r)],2));return this.onRender?.(),e.default?(d(),u("div",{key:3,class:i([`${t}-spin-container`,this.themeClass]),style:f(this.cssVars)},[z("div",{class:i([`${t}-spin-content`,this.active&&`${t}-spin-content--spinning`,this.contentClass]),style:f(this.contentStyle)},[p(()=>e.default?.())],6),V(N,{name:"fade-in-transition"},{default:()=>this.active?l:null},1024)],6)):l}});export{X as S};
