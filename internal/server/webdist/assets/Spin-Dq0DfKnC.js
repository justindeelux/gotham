import{V as g,W as a,aK as $,ak as b,d as w,dR as B,X as k,o as d,c as u,_ as p,ad as i,a as z,ac as m,n as R,ci as T,e as V,$ as N,am as P,aC as W,z as y,s as _,ax as E,dS as I,a2 as O,a1 as j}from"./index-DH2iXTRO.js";import{u as K}from"./use-compitable-8erEc1hS.js";var L=g([g("@keyframes spin-rotate",`
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
 `)])]);const D={small:20,medium:18,large:16},H={...k.props,contentClass:String,contentStyle:[Object,String],description:String,size:{type:[String,Number],default:"medium"},show:{type:Boolean,default:!0},rotate:{type:Boolean,default:!0},spinning:{type:Boolean,validator:()=>!0,default:void 0},delay:Number,...B,strokeWidth:Number};var Y=w({name:"Spin",props:H,slots:Object,setup(e){const{mergedClsPrefixRef:t,inlineThemeDisabled:o}=N(e),f=k("Spin","-spin",L,I,e,t),r=y(()=>{const{size:s}=e,{common:{cubicBezierEaseInOut:n},self:c}=f.value,{opacitySpinning:S,color:C,textColor:x}=c;return{"--n-bezier":n,"--n-opacity-spinning":S,"--n-size":typeof s=="number"?O(s):c[j("size",s)],"--n-color":C,"--n-text-color":x}}),l=o?P("spin",y(()=>{const{size:s}=e;return typeof s=="number"?String(s):s[0]}),r,e):void 0,v=K(e,["spinning","show"]),h=_(!1);return W(s=>{let n;if(v.value){const{delay:c}=e;if(c){n=window.setTimeout(()=>{h.value=!0},c),s(()=>{clearTimeout(n)});return}}h.value=v.value}),{mergedClsPrefix:t,active:h,mergedStrokeWidth:y(()=>{const{strokeWidth:s}=e;if(s!==void 0)return s;const{size:n}=e;return D[typeof n=="number"?"medium":n]}),cssVars:o?void 0:r,themeClass:l?.themeClass,onRender:l?.onRender}},render(){const{$slots:e,mergedClsPrefix:t,description:o}=this,f=e.icon&&this.rotate,r=(o||e.description)&&(d(),u("div",{class:i(`${t}-spin-description`)},[p(()=>o||e.description?.())],2)),l=e.icon?(d(),u("div",{key:1,class:i([`${t}-spin-body`,this.themeClass])},[z("div",{class:i([`${t}-spin`,f&&`${t}-spin--rotate`]),style:m(e.default?"":this.cssVars)},[p(()=>e.icon())],6),p(()=>r)],2)):(d(),u("div",{key:2,class:i([`${t}-spin-body`,this.themeClass])},[(d(),R(T,{clsPrefix:t,style:m(e.default?"":this.cssVars),stroke:this.stroke,"stroke-width":this.mergedStrokeWidth,radius:this.radius,scale:this.scale,class:i(`${t}-spin`)},null,8,["clsPrefix","style","stroke","stroke-width","radius","scale","class"])),p(()=>r)],2));return this.onRender?.(),e.default?(d(),u("div",{key:3,class:i([`${t}-spin-container`,this.themeClass]),style:m(this.cssVars)},[z("div",{class:i([`${t}-spin-content`,this.active&&`${t}-spin-content--spinning`,this.contentClass]),style:m(this.contentStyle)},[p(()=>e.default?.())],6),V(E,{name:"fade-in-transition"},{default:()=>this.active?l:null},1024)],6)):l}});export{Y as S};
