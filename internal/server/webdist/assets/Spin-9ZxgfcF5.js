<<<<<<<< HEAD:internal/server/webdist/assets/Spin-CFouQ4ZX.js
import{P as g,Q as a,aE as w,ah as b,d as $,dw as B,S,o as d,c as u,V as p,aa as i,a as z,a9 as m,m as T,ca as V,e as P,W as R,aj as N,aw as W,y,q as E,ar as _,dx as j,Z as I,Y as O}from"./index-CpGISgfF.js";import{u as L}from"./text-69wK2Vbo.js";var D=g([g("@keyframes spin-rotate",`
========
import{Q as g,S as a,aA as $,ad as b,d as w,du as T,T as S,o as d,c as u,W as p,a6 as i,a as z,a5 as m,m as B,c7 as R,e as V,X as N,af as P,as as W,y,q as _,an as E,dv as I,_ as O,Z as j}from"./index-B9tI3yZl.js";import{u as L}from"./text-CuzXJaa8.js";var D=g([g("@keyframes spin-rotate",`
>>>>>>>> d81c43b0 (Web: split shell-dash-auth components with per-mount drawer state (JUS-24)):internal/server/webdist/assets/Spin-9ZxgfcF5.js
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
 `,[w()])]),a("spin-body",`
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
<<<<<<<< HEAD:internal/server/webdist/assets/Spin-CFouQ4ZX.js
 `)])]);const H={small:20,medium:18,large:16},K={...S.props,contentClass:String,contentStyle:[Object,String],description:String,size:{type:[String,Number],default:"medium"},show:{type:Boolean,default:!0},rotate:{type:Boolean,default:!0},spinning:{type:Boolean,validator:()=>!0,default:void 0},delay:Number,...B,strokeWidth:Number};var M=$({name:"Spin",props:K,slots:Object,setup(e){const{mergedClsPrefixRef:t,inlineThemeDisabled:o}=R(e),f=S("Spin","-spin",D,j,e,t),r=y(()=>{const{size:s}=e,{common:{cubicBezierEaseInOut:n},self:c}=f.value,{opacitySpinning:k,color:x,textColor:C}=c;return{"--n-bezier":n,"--n-opacity-spinning":k,"--n-size":typeof s=="number"?I(s):c[O("size",s)],"--n-color":x,"--n-text-color":C}}),l=o?N("spin",y(()=>{const{size:s}=e;return typeof s=="number"?String(s):s[0]}),r,e):void 0,v=L(e,["spinning","show"]),h=E(!1);return W(s=>{let n;if(v.value){const{delay:c}=e;if(c){n=window.setTimeout(()=>{h.value=!0},c),s(()=>{clearTimeout(n)});return}}h.value=v.value}),{mergedClsPrefix:t,active:h,mergedStrokeWidth:y(()=>{const{strokeWidth:s}=e;if(s!==void 0)return s;const{size:n}=e;return H[typeof n=="number"?"medium":n]}),cssVars:o?void 0:r,themeClass:l?.themeClass,onRender:l?.onRender}},render(){const{$slots:e,mergedClsPrefix:t,description:o}=this,f=e.icon&&this.rotate,r=(o||e.description)&&(d(),u("div",{class:i(`${t}-spin-description`)},[p(()=>o||e.description?.())],2)),l=e.icon?(d(),u("div",{key:1,class:i([`${t}-spin-body`,this.themeClass])},[z("div",{class:i([`${t}-spin`,f&&`${t}-spin--rotate`]),style:m(e.default?"":this.cssVars)},[p(()=>e.icon())],6),p(()=>r)],2)):(d(),u("div",{key:2,class:i([`${t}-spin-body`,this.themeClass])},[(d(),T(V,{clsPrefix:t,style:m(e.default?"":this.cssVars),stroke:this.stroke,"stroke-width":this.mergedStrokeWidth,radius:this.radius,scale:this.scale,class:i(`${t}-spin`)},null,8,["clsPrefix","style","stroke","stroke-width","radius","scale","class"])),p(()=>r)],2));return this.onRender?.(),e.default?(d(),u("div",{key:3,class:i([`${t}-spin-container`,this.themeClass]),style:m(this.cssVars)},[z("div",{class:i([`${t}-spin-content`,this.active&&`${t}-spin-content--spinning`,this.contentClass]),style:m(this.contentStyle)},[p(()=>e.default?.())],6),P(_,{name:"fade-in-transition"},{default:()=>this.active?l:null},1024)],6)):l}});export{M as S};
========
 `)])]);const H={small:20,medium:18,large:16},K={...S.props,contentClass:String,contentStyle:[Object,String],description:String,size:{type:[String,Number],default:"medium"},show:{type:Boolean,default:!0},rotate:{type:Boolean,default:!0},spinning:{type:Boolean,validator:()=>!0,default:void 0},delay:Number,...T,strokeWidth:Number};var A=w({name:"Spin",props:K,slots:Object,setup(e){const{mergedClsPrefixRef:t,inlineThemeDisabled:o}=N(e),f=S("Spin","-spin",D,I,e,t),r=y(()=>{const{size:s}=e,{common:{cubicBezierEaseInOut:n},self:c}=f.value,{opacitySpinning:k,color:C,textColor:x}=c;return{"--n-bezier":n,"--n-opacity-spinning":k,"--n-size":typeof s=="number"?O(s):c[j("size",s)],"--n-color":C,"--n-text-color":x}}),l=o?P("spin",y(()=>{const{size:s}=e;return typeof s=="number"?String(s):s[0]}),r,e):void 0,v=L(e,["spinning","show"]),h=_(!1);return W(s=>{let n;if(v.value){const{delay:c}=e;if(c){n=window.setTimeout(()=>{h.value=!0},c),s(()=>{clearTimeout(n)});return}}h.value=v.value}),{mergedClsPrefix:t,active:h,mergedStrokeWidth:y(()=>{const{strokeWidth:s}=e;if(s!==void 0)return s;const{size:n}=e;return H[typeof n=="number"?"medium":n]}),cssVars:o?void 0:r,themeClass:l?.themeClass,onRender:l?.onRender}},render(){const{$slots:e,mergedClsPrefix:t,description:o}=this,f=e.icon&&this.rotate,r=(o||e.description)&&(d(),u("div",{class:i(`${t}-spin-description`)},[p(()=>o||e.description?.())],2)),l=e.icon?(d(),u("div",{key:1,class:i([`${t}-spin-body`,this.themeClass])},[z("div",{class:i([`${t}-spin`,f&&`${t}-spin--rotate`]),style:m(e.default?"":this.cssVars)},[p(()=>e.icon())],6),p(()=>r)],2)):(d(),u("div",{key:2,class:i([`${t}-spin-body`,this.themeClass])},[(d(),B(R,{clsPrefix:t,style:m(e.default?"":this.cssVars),stroke:this.stroke,"stroke-width":this.mergedStrokeWidth,radius:this.radius,scale:this.scale,class:i(`${t}-spin`)},null,8,["clsPrefix","style","stroke","stroke-width","radius","scale","class"])),p(()=>r)],2));return this.onRender?.(),e.default?(d(),u("div",{key:3,class:i([`${t}-spin-container`,this.themeClass]),style:m(this.cssVars)},[z("div",{class:i([`${t}-spin-content`,this.active&&`${t}-spin-content--spinning`,this.contentClass]),style:m(this.contentStyle)},[p(()=>e.default?.())],6),V(E,{name:"fade-in-transition"},{default:()=>this.active?l:null},1024)],6)):l}});export{A as S};
>>>>>>>> d81c43b0 (Web: split shell-dash-auth components with per-mount drawer state (JUS-24)):internal/server/webdist/assets/Spin-9ZxgfcF5.js
