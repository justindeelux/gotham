import{u as R,S as B,t as P}from"./text-Bp-uvbgw.js";import{p as b,q as c,s as V,v as S,d as x,x as N,y as w,g as d,c as h,z as m,A as r,D as z,E as y,h as E,L as I,a as g,T as L,F as O,G as W,H as v,I as j,r as A,J as D,K as H,M as K,u as M,o as q,w as _,b as k,f as F,i as G}from"./index-BJWxwDGj.js";var J=b([b("@keyframes spin-rotate",`
 from {
 transform: rotate(0);
 }
 to {
 transform: rotate(360deg);
 }
 `),c("spin-container",`
 position: relative;
 `,[c("spin-body",`
 position: absolute;
 top: 50%;
 left: 50%;
 transform: translateX(-50%) translateY(-50%);
 `,[V()])]),c("spin-body",`
 display: inline-flex;
 align-items: center;
 justify-content: center;
 flex-direction: column;
 `),c("spin",`
 display: inline-flex;
 height: var(--n-size);
 width: var(--n-size);
 font-size: var(--n-size);
 color: var(--n-color);
 `,[S("rotate",`
 animation: spin-rotate 2s linear infinite;
 `)]),c("spin-description",`
 display: inline-block;
 font-size: var(--n-font-size);
 color: var(--n-text-color);
 transition: color .3s var(--n-bezier);
 margin-top: 8px;
 `),c("spin-content",`
 opacity: 1;
 transition: opacity .3s var(--n-bezier);
 pointer-events: all;
 `,[S("spinning",`
 user-select: none;
 -webkit-user-select: none;
 pointer-events: none;
 opacity: var(--n-opacity-spinning);
 `)])]);const U={small:20,medium:18,large:16},X={...w.props,contentClass:String,contentStyle:[Object,String],description:String,size:{type:[String,Number],default:"medium"},show:{type:Boolean,default:!0},rotate:{type:Boolean,default:!0},spinning:{type:Boolean,validator:()=>!0,default:void 0},delay:Number,...N,strokeWidth:Number};var Y=x({name:"Spin",props:X,slots:Object,setup(e){const{mergedClsPrefixRef:t,inlineThemeDisabled:a}=O(e),u=w("Spin","-spin",J,D,e,t),s=v(()=>{const{size:n}=e,{common:{cubicBezierEaseInOut:o},self:f}=u.value,{opacitySpinning:C,color:T,textColor:$}=f;return{"--n-bezier":o,"--n-opacity-spinning":C,"--n-size":typeof n=="number"?H(n):f[K("size",n)],"--n-color":T,"--n-text-color":$}}),i=a?W("spin",v(()=>{const{size:n}=e;return typeof n=="number"?String(n):n[0]}),s,e):void 0,p=R(e,["spinning","show"]),l=A(!1);return j(n=>{let o;if(p.value){const{delay:f}=e;if(f){o=window.setTimeout(()=>{l.value=!0},f),n(()=>{clearTimeout(o)});return}}l.value=p.value}),{mergedClsPrefix:t,active:l,mergedStrokeWidth:v(()=>{const{strokeWidth:n}=e;if(n!==void 0)return n;const{size:o}=e;return U[typeof o=="number"?"medium":o]}),cssVars:a?void 0:s,themeClass:i?.themeClass,onRender:i?.onRender}},render(){const{$slots:e,mergedClsPrefix:t,description:a}=this,u=e.icon&&this.rotate,s=(a||e.description)&&(d(),h("div",{class:r(`${t}-spin-description`)},[m(()=>a||e.description?.())],2)),i=e.icon?(d(),h("div",{key:1,class:r([`${t}-spin-body`,this.themeClass])},[z("div",{class:r([`${t}-spin`,u&&`${t}-spin--rotate`]),style:y(e.default?"":this.cssVars)},[m(()=>e.icon())],6),m(()=>s)],2)):(d(),h("div",{key:2,class:r([`${t}-spin-body`,this.themeClass])},[(d(),E(I,{clsPrefix:t,style:y(e.default?"":this.cssVars),stroke:this.stroke,"stroke-width":this.mergedStrokeWidth,radius:this.radius,scale:this.scale,class:r(`${t}-spin`)},null,8,["clsPrefix","style","stroke","stroke-width","radius","scale","class"])),m(()=>s)],2));return this.onRender?.(),e.default?(d(),h("div",{key:3,class:r([`${t}-spin-container`,this.themeClass]),style:y(this.cssVars)},[z("div",{class:r([`${t}-spin-content`,this.active&&`${t}-spin-content--spinning`,this.contentClass]),style:y(this.contentStyle)},[m(()=>e.default?.())],6),g(L,{name:"fade-in-transition"},{default:()=>this.active?i:null},1024)],6)):i}});const Q={class:"auth-page"},te=x({__name:"OAuthCallbackPage",setup(e){const t=M(),a=F();function u(){const s=window.location.hash.replace(/^#/,"");if(!s)return null;const i=new URLSearchParams(s),p=i.get("access_token"),l=i.get("refresh_token");return!p||!l?null:{access_token:p,refresh_token:l}}return q(async()=>{const s=u();if(!s){await a.replace({path:"/login",query:{error:"oauth_failed"}});return}t.setSession(s),window.history.replaceState(null,"","/oauth/callback"),await a.replace({path:"/dashboard"})}),(s,i)=>(d(),h("div",Q,[g(k(B),{vertical:"",align:"center",size:16},{default:_(()=>[g(k(Y),{size:"large"}),g(k(P),{depth:"3"},{default:_(()=>[...i[0]||(i[0]=[G("Signing you in…",-1)])]),_:1})]),_:1})]))}});export{te as default};
