import{a2 as g,a3 as a,aW as $,av as b,d as w,ff as B,a4 as k,o as d,c as u,a6 as p,ao as i,a as z,an as f,x as T,dq as R,f as V,aJ as N,a7 as P,ax as W,aO as O,g as y,p as _,fg as E,aa as I,a9 as j}from"./index-CAh1mszj.js";import{u as L}from"./use-compitable-BqEBHjcV.js";var D=g([g("@keyframes spin-rotate",`
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
 `)])]);const H={small:20,medium:18,large:16},K={...k.props,contentClass:String,contentStyle:[Object,String],description:String,size:{type:[String,Number],default:"medium"},show:{type:Boolean,default:!0},rotate:{type:Boolean,default:!0},spinning:{type:Boolean,validator:()=>!0,default:void 0},delay:Number,...B,strokeWidth:Number};var M=w({name:"Spin",props:K,slots:Object,setup(e){const{mergedClsPrefixRef:t,inlineThemeDisabled:o}=P(e),m=k("Spin","-spin",D,E,e,t),r=y(()=>{const{size:s}=e,{common:{cubicBezierEaseInOut:n},self:c}=m.value,{opacitySpinning:x,color:S,textColor:C}=c;return{"--n-bezier":n,"--n-opacity-spinning":x,"--n-size":typeof s=="number"?I(s):c[j("size",s)],"--n-color":S,"--n-text-color":C}}),l=o?W("spin",y(()=>{const{size:s}=e;return typeof s=="number"?String(s):s[0]}),r,e):void 0,v=L(e,["spinning","show"]),h=_(!1);return O(s=>{let n;if(v.value){const{delay:c}=e;if(c){n=window.setTimeout(()=>{h.value=!0},c),s(()=>{clearTimeout(n)});return}}h.value=v.value}),{mergedClsPrefix:t,active:h,mergedStrokeWidth:y(()=>{const{strokeWidth:s}=e;if(s!==void 0)return s;const{size:n}=e;return H[typeof n=="number"?"medium":n]}),cssVars:o?void 0:r,themeClass:l?.themeClass,onRender:l?.onRender}},render(){const{$slots:e,mergedClsPrefix:t,description:o}=this,m=e.icon&&this.rotate,r=(o||e.description)&&(d(),u("div",{class:i(`${t}-spin-description`)},[p(()=>o||e.description?.())],2)),l=e.icon?(d(),u("div",{key:1,class:i([`${t}-spin-body`,this.themeClass])},[z("div",{class:i([`${t}-spin`,m&&`${t}-spin--rotate`]),style:f(e.default?"":this.cssVars)},[p(()=>e.icon())],6),p(()=>r)],2)):(d(),u("div",{key:2,class:i([`${t}-spin-body`,this.themeClass])},[(d(),T(R,{clsPrefix:t,style:f(e.default?"":this.cssVars),stroke:this.stroke,"stroke-width":this.mergedStrokeWidth,radius:this.radius,scale:this.scale,class:i(`${t}-spin`)},null,8,["clsPrefix","style","stroke","stroke-width","radius","scale","class"])),p(()=>r)],2));return this.onRender?.(),e.default?(d(),u("div",{key:3,class:i([`${t}-spin-container`,this.themeClass]),style:f(this.cssVars)},[z("div",{class:i([`${t}-spin-content`,this.active&&`${t}-spin-content--spinning`,this.contentClass]),style:f(this.contentStyle)},[p(()=>e.default?.())],6),V(N,{name:"fade-in-transition"},{default:()=>this.active?l:null},1024)],6)):l}});export{M as S};
